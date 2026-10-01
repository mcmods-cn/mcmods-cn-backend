package serverprobe

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type test015ChangingResolver struct {
	mu    sync.Mutex
	ips   []net.IPAddr
	calls int
}

func (r *test015ChangingResolver) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	return "", nil, errors.New("owned fixture has no SRV")
}
func (r *test015ChangingResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls > 1 {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	return r.ips, nil
}

func newTEST015TCP(t *testing.T, count int, serve func(int, net.Conn) error) (string, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan error, 1)
	go func() {
		defer listener.Close()
		for i := 0; i < count; i++ {
			conn, err := listener.Accept()
			if err != nil {
				done <- err
				return
			}
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			err = serve(i, conn)
			_ = conn.Close()
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	return listener.Addr().String(), done
}

func test015ReadHandshake(conn *configurationPacketConn, wantHost string, wantState int32) error {
	id, payload, err := conn.readPacket()
	if err != nil || id != 0 {
		return fmt.Errorf("handshake id=%d: %w", id, err)
	}
	reader := bytes.NewReader(payload)
	if _, err = readVarInt(reader); err != nil {
		return err
	}
	host, err := readProtocolString(reader, 255)
	if err != nil {
		return err
	}
	var port uint16
	if err = binary.Read(reader, binary.BigEndian, &port); err != nil {
		return err
	}
	state, err := readVarInt(reader)
	if err != nil || host != wantHost || state != wantState || port == 0 || reader.Len() != 0 {
		return fmt.Errorf("actual handshake host=%s port=%d state=%d tail=%d err=%v", host, port, state, reader.Len(), err)
	}
	return nil
}

func TestTEST015MixedDNSAnswersRejectBeforeAnyDial(t *testing.T) {
	for _, private := range []string{"127.0.0.1", "10.1.2.3", "172.16.1.1", "192.168.1.1", "169.254.169.254", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1"} {
		t.Run(private, func(t *testing.T) {
			resolver := &test015ChangingResolver{ips: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}, {IP: net.ParseIP(private)}}}
			called := false
			_, err := probeWithTransport(context.Background(), "mixed.invalid:25565", false, resolver, func(context.Context, string, string) (net.Conn, error) {
				called = true
				return nil, errors.New("must never dial rejected DNS")
			})
			if err == nil || called || resolver.calls != 1 {
				t.Fatalf("mixed public/private DNS accepted: err=%v dial=%t lookups=%d", err, called, resolver.calls)
			}
		})
	}
}

func TestTEST015StatusAndConfigurationTCPShareOnePinnedDNSResolution(t *testing.T) {
	local, finished := newTEST015TCP(t, 2, func(index int, connection net.Conn) error {
		packet := &configurationPacketConn{conn: connection, reader: bufio.NewReader(connection), compressionThreshold: -1}
		if index == 0 {
			if err := test015ReadHandshake(packet, "rebind.invalid", 1); err != nil {
				return err
			}
			id, payload, err := packet.readPacket()
			if err != nil || id != 0 || len(payload) != 0 {
				return fmt.Errorf("status request id=%d bytes=%d err=%v", id, len(payload), err)
			}
			body := new(bytes.Buffer)
			_ = writeProtocolString(body, `{"version":{"name":"1.21.1","protocol":767},"players":{"max":20,"online":3},"description":{"text":"TEST015 real TCP"}}`)
			return packet.writePacket(0, body.Bytes())
		}
		if err := test015ReadHandshake(packet, "rebind.invalid", 2); err != nil {
			return err
		}
		if id, _, err := packet.readPacket(); err != nil || id != 0 {
			return fmt.Errorf("login start id=%d err=%v", id, err)
		}
		login := new(bytes.Buffer)
		_, _ = login.Write(make([]byte, 16))
		_ = writeProtocolString(login, "MCModsProbe")
		writeVarInt(login, 0)
		if err := packet.writePacket(2, login.Bytes()); err != nil {
			return err
		}
		if id, _, err := packet.readPacket(); err != nil || id != 3 {
			return fmt.Errorf("actual login ack id=%d err=%v", id, err)
		}
		if id, _, err := packet.readPacket(); err != nil || id != 2 {
			return fmt.Errorf("actual channel registration id=%d err=%v", id, err)
		}
		registry := new(bytes.Buffer)
		_ = writeProtocolString(registry, "minecraft:item")
		writeVarInt(registry, 1)
		_ = writeProtocolString(registry, "test015_mod:item")
		_ = registry.WriteByte(0)
		if err := packet.writePacket(7, registry.Bytes()); err != nil {
			return err
		}
		return packet.writePacket(3, nil)
	})
	_, port, err := net.SplitHostPort(local)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &test015ChangingResolver{ips: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}}
	dials := []string{}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != net.JoinHostPort("1.1.1.1", port) {
			return nil, fmt.Errorf("unsafe second resolution/hostname dial: %s %s", network, address)
		}
		dials = append(dials, address)
		return (&net.Dialer{}).DialContext(ctx, "tcp", local)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := probeWithTransport(ctx, "rebind.invalid:"+port, true, resolver, dial)
	if err != nil || !result.Online || result.HandshakeHost != "rebind.invalid" || result.PlayersOnline != 3 || result.MinecraftVersion != "1.21.1" || len(result.Mods) != 1 || result.Mods[0].ID != "test015_mod" || result.Mods[0].Source != "configuration" || result.Mods[0].Confidence == "" || resolver.calls != 1 || len(dials) != 2 {
		t.Fatalf("real pinned status/configuration flow: result=%+v err=%v DNS=%d dials=%v", result, err, resolver.calls, dials)
	}
	if serverErr := <-finished; serverErr != nil {
		t.Fatal(serverErr)
	}
}

func TestTEST015StatusProbeCancellationInterruptsConnectedRead(t *testing.T) {
	ready, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	local, finished := newTEST015TCP(t, 1, func(_ int, connection net.Conn) error {
		packet := &configurationPacketConn{conn: connection, reader: bufio.NewReader(connection), compressionThreshold: -1}
		if err := test015ReadHandshake(packet, "cancel.invalid", 1); err != nil {
			return err
		}
		if _, _, err := packet.readPacket(); err != nil {
			return err
		}
		close(ready)
		<-release
		return nil
	})
	_, port, _ := net.SplitHostPort(local)
	resolver := &test015ChangingResolver{ips: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := probeWithTransport(ctx, "cancel.invalid:"+port, false, resolver, func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != net.JoinHostPort("1.1.1.1", port) {
				return nil, fmt.Errorf("not pinned: %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, local)
		})
		result <- err
	}()
	select {
	case <-ready:
	case err := <-finished:
		t.Fatalf("server not reached: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("owned TCP handshake did not arrive")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled blocked status read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not release connected status read within one second")
	}
}

func TestTEST015OversizedStatusFrameFailsBeforePayloadRead(t *testing.T) {
	local, finished := newTEST015TCP(t, 1, func(_ int, connection net.Conn) error {
		packet := &configurationPacketConn{conn: connection, reader: bufio.NewReader(connection), compressionThreshold: -1}
		if err := test015ReadHandshake(packet, "oversize.invalid", 1); err != nil {
			return err
		}
		if _, _, err := packet.readPacket(); err != nil {
			return err
		}
		header := new(bytes.Buffer)
		writeVarInt(header, int32(maxStatusPacketBytes+1))
		_, err := connection.Write(header.Bytes())
		return err
	})
	_, port, _ := net.SplitHostPort(local)
	resolver := &test015ChangingResolver{ips: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}}
	_, err := probeWithTransport(context.Background(), "oversize.invalid:"+port, false, resolver, func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != net.JoinHostPort("1.1.1.1", port) {
			return nil, fmt.Errorf("not pinned: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, local)
	})
	if err == nil || !strings.Contains(err.Error(), "invalid status packet size") {
		t.Fatalf("oversized frame was not rejected at declared length: %v", err)
	}
	if serverErr := <-finished; serverErr != nil {
		t.Fatal(serverErr)
	}
}

func TestTEST015ConfigurationCancellationReleasesProcessSlot(t *testing.T) {
	ready, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	local, finished := newTEST015TCP(t, 2, func(index int, connection net.Conn) error {
		packet := &configurationPacketConn{conn: connection, reader: bufio.NewReader(connection), compressionThreshold: -1}
		if index == 0 {
			if err := test015ReadHandshake(packet, "config-cancel.invalid", 1); err != nil {
				return err
			}
			if _, _, err := packet.readPacket(); err != nil {
				return err
			}
			body := new(bytes.Buffer)
			_ = writeProtocolString(body, `{"version":{"name":"1.21.1","protocol":767},"players":{"max":20,"online":3},"description":{"text":"TEST015 cancel configuration"}}`)
			return packet.writePacket(0, body.Bytes())
		}
		if err := test015ReadHandshake(packet, "config-cancel.invalid", 2); err != nil {
			return err
		}
		if _, _, err := packet.readPacket(); err != nil {
			return err
		}
		close(ready)
		<-release
		return nil
	})
	_, port, _ := net.SplitHostPort(local)
	resolver := &test015ChangingResolver{ips: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	result := make(chan outcome, 1)
	go func() {
		value, err := probeWithTransport(ctx, "config-cancel.invalid:"+port, true, resolver, func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != net.JoinHostPort("1.1.1.1", port) {
				return nil, fmt.Errorf("not pinned: %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, local)
		})
		result <- outcome{value, err}
	}()
	select {
	case <-ready:
	case err := <-finished:
		t.Fatalf("server configuration phase not reached: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("owned configuration handshake did not arrive")
	}
	for i := 1; i < maximumConcurrentConfigurationProbes; i++ {
		releaseSlot, err := acquireConfigurationProbeSlot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(releaseSlot)
	}
	cancel()
	select {
	case got := <-result:
		if got.err != nil || !got.result.Online || got.result.DetectionDiagnostic == "" || got.result.ModListComplete {
			t.Fatalf("cancelled configuration must retain only prior status with an incomplete diagnostic: %+v err=%v", got.result, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not release connected configuration read within one second")
	}
	releaseSlot, err := acquireConfigurationProbeSlot(context.Background())
	if err != nil {
		t.Fatalf("cancelled probe leaked its process-wide configuration slot: %v", err)
	}
	releaseSlot()
}

func TestTEST015CompressedConfigurationFramesOverTCP(t *testing.T) {
	compress := func(raw []byte) []byte {
		var encoded bytes.Buffer
		writer := zlib.NewWriter(&encoded)
		if _, err := writer.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return encoded.Bytes()
	}
	for _, sample := range []struct {
		name     string
		declared int32
		encoded  []byte
		want     string
	}{
		{"valid", 4, compress([]byte{7, 'a', 'b', 'c'}), ""},
		{"zero-means-uncompressed", 0, []byte{7, 'a', 'b', 'c'}, ""},
		{"declared-too-large-before-zlib", maxConfigurationPacketBytes + 1, []byte{0}, "expands beyond limit"},
		{"negative-expanded-length", -1, []byte{0}, "expands beyond limit"},
		{"malformed-zlib", 4, []byte{0, 0, 0}, "zlib"},
		{"truncated-zlib", 4, compress([]byte{7, 'a', 'b', 'c'})[:4], "unexpected EOF"},
		{"declared-actual-mismatch", 3, compress([]byte{7, 'a', 'b', 'c'}), "length mismatch"},
		{"small-declaration-expansion-bomb", 1, compress(bytes.Repeat([]byte{'x'}, 64<<20)), "length mismatch"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			frame := new(bytes.Buffer)
			writeVarInt(frame, sample.declared)
			_, _ = frame.Write(sample.encoded)
			local, finished := newTEST015TCP(t, 1, func(_ int, connection net.Conn) error {
				header := new(bytes.Buffer)
				writeVarInt(header, int32(frame.Len()))
				_, err := connection.Write(append(header.Bytes(), frame.Bytes()...))
				return err
			})
			connection, err := net.DialTimeout("tcp", local, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
			packet := &configurationPacketConn{conn: connection, reader: bufio.NewReader(connection), compressionThreshold: 1}
			var allocationBefore, allocationAfter runtime.MemStats
			if sample.name == "small-declaration-expansion-bomb" {
				// The 64 MiB hostile source and compressed fixture already exist.
				// Measure only receiving/decoding, not constructing attacker input.
				runtime.ReadMemStats(&allocationBefore)
			}
			id, payload, err := packet.readPacket()
			if sample.name == "small-declaration-expansion-bomb" {
				runtime.ReadMemStats(&allocationAfter)
				allocated := allocationAfter.TotalAlloc - allocationBefore.TotalAlloc
				if allocated > 8*maxConfigurationPacketBytes {
					t.Fatalf("bounded TCP decoder allocated %d bytes for a declared-one-byte/64-MiB expansion bomb, limit=%d", allocated, 8*maxConfigurationPacketBytes)
				}
				t.Logf("actual receive/decode allocation=%d bytes, budget=%d bytes", allocated, 8*maxConfigurationPacketBytes)
			}
			if sample.want == "" {
				if err != nil || id != 7 || string(payload) != "abc" {
					t.Fatalf("valid compressed/uncompressed TCP frame id=%d payload=%q err=%v", id, payload, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), sample.want) || len(payload) != 0 {
				t.Fatalf("hostile compressed TCP frame payload=%d err=%v want=%q", len(payload), err, sample.want)
			}
			if serverErr := <-finished; serverErr != nil {
				t.Fatal(serverErr)
			}
		})
	}
}

// The test changes only the final TCP destination to an owned loopback listener.
// DNS validation, fixed-IP dialing, status and configuration decoding are production paths.
func test015ConfigurationTCP(t *testing.T, serve func(*configurationPacketConn) error) (Result, error, <-chan error) {
	t.Helper()
	local, finished := newTEST015TCP(t, 2, func(index int, connection net.Conn) error {
		packet := &configurationPacketConn{conn: connection, reader: bufio.NewReader(connection), compressionThreshold: -1}
		state := int32(1)
		if index != 0 {
			state = 2
		}
		if err := test015ReadHandshake(packet, "bounded.invalid", state); err != nil {
			return err
		}
		if id, _, err := packet.readPacket(); err != nil || id != 0 {
			return fmt.Errorf("initial status/login packet id=%d err=%v", id, err)
		}
		if index == 0 {
			body := new(bytes.Buffer)
			_ = writeProtocolString(body, `{"version":{"name":"1.21.1","protocol":767},"players":{"max":20,"online":3},"description":{"text":"TEST015 bounded TCP"}}`)
			return packet.writePacket(0, body.Bytes())
		}
		login := new(bytes.Buffer)
		_, _ = login.Write(make([]byte, 16))
		_ = writeProtocolString(login, "MCModsProbe")
		writeVarInt(login, 0)
		if err := packet.writePacket(2, login.Bytes()); err != nil {
			return err
		}
		if id, _, err := packet.readPacket(); err != nil || id != 3 {
			return fmt.Errorf("login acknowledgement id=%d err=%v", id, err)
		}
		if id, _, err := packet.readPacket(); err != nil || id != 2 {
			return fmt.Errorf("channel registration id=%d err=%v", id, err)
		}
		return serve(packet)
	})
	_, port, _ := net.SplitHostPort(local)
	resolver := &test015ChangingResolver{ips: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := probeWithTransport(ctx, "bounded.invalid:"+port, true, resolver, func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != net.JoinHostPort("1.1.1.1", port) {
			return nil, fmt.Errorf("not pinned: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, local)
	})
	return result, err, finished
}

func test015FabricPacket(packet *configurationPacketConn, data []byte) error {
	payload := new(bytes.Buffer)
	_ = writeProtocolString(payload, fabricDirectRegistryChannel)
	_, _ = payload.Write(data)
	return packet.writePacket(1, payload.Bytes())
}

func TestTEST015FabricRegistryAggregateBoundaryOverTCP(t *testing.T) {
	valid := new(bytes.Buffer)
	writeVarInt(valid, 1)
	_ = writeProtocolString(valid, "")
	writeVarInt(valid, 1)
	_ = writeProtocolString(valid, "item")
	writeVarInt(valid, 1)
	_ = writeProtocolString(valid, "test015_fabric")
	writeVarInt(valid, 1)
	writeVarInt(valid, 0)
	writeVarInt(valid, 1)
	_ = writeProtocolString(valid, "item")
	// Zero padding is explicitly allowed by the actual Fabric decoder. Exactly
	// eight MiB must be admitted and parsed, not merely rejected as malformed.
	padded := make([]byte, maxFabricRegistrySyncBytes)
	copy(padded, valid.Bytes())
	for _, sample := range []struct {
		name     string
		tooLarge bool
		legacy64 bool
	}{
		{"exact-eight-MiB-valid-with-padding", false, false},
		{"eight-MiB-plus-one", true, false},
		{"legacy-sixty-four-MiB-stream-stopped-early", true, true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var sent atomic.Int64
			result, err, finished := test015ConfigurationTCP(t, func(packet *configurationPacketConn) error {
				for offset := 0; offset < len(padded); offset += 1 << 20 {
					if err := test015FabricPacket(packet, padded[offset:offset+(1<<20)]); err != nil {
						return err
					}
					sent.Add(1 << 20)
				}
				if sample.tooLarge {
					if err := test015FabricPacket(packet, []byte{0}); err != nil {
						return err
					}
					sent.Add(1)
					if sample.legacy64 {
						for sent.Load() < 64<<20 {
							if err := test015FabricPacket(packet, padded[:1<<20]); err != nil {
								// The receiver must close before consuming the advertised legacy stream.
								if sent.Load() > 10<<20 {
									return fmt.Errorf("legacy stream consumed %d bytes before closure", sent.Load())
								}
								return nil
							}
							sent.Add(1 << 20)
						}
						return errors.New("legacy 64 MiB stream was fully accepted")
					}
					var tail [1]byte
					_, err := packet.conn.Read(tail[:])
					if err == nil {
						return errors.New("oversized stream did not close")
					}
					return nil
				}
				if err := test015FabricPacket(packet, nil); err != nil {
					return err
				}
				id, payload, err := packet.readPacket()
				if err != nil || id != 2 {
					return fmt.Errorf("Fabric completion acknowledgement id=%d err=%v", id, err)
				}
				channel, err := readProtocolString(bytes.NewReader(payload), 32767)
				if err != nil || channel != "fabric:registry/sync/complete" {
					return fmt.Errorf("Fabric completion channel=%q err=%v", channel, err)
				}
				return packet.writePacket(3, nil)
			})
			if err != nil || !result.Online || result.ModListComplete || result.Loader != "fabric" {
				t.Fatalf("bounded full status/configuration TCP result=%+v err=%v", result, err)
			}
			if sample.tooLarge {
				if !strings.Contains(result.DetectionDiagnostic, "同步数据超过安全限制") || len(result.Mods) != 0 {
					t.Fatalf("excess Fabric data produced usable evidence: %+v", result)
				}
			} else if len(result.Mods) != 1 || result.Mods[0].ID != "test015_fabric" || result.Mods[0].Confidence != "inferred" {
				t.Fatalf("exact-limit valid Fabric evidence not parsed: %+v", result)
			}
			if serverErr := <-finished; serverErr != nil {
				t.Fatal(serverErr)
			}
			t.Logf("actual Fabric bytes sent=%d; complete-JAR-list claim remains false", sent.Load())
		})
	}
}

func TestTEST015MillionRegistryCountOverConfigurationTCP(t *testing.T) {
	result, err, finished := test015ConfigurationTCP(t, func(packet *configurationPacketConn) error {
		payload := new(bytes.Buffer)
		_ = writeProtocolString(payload, "minecraft:item")
		writeVarInt(payload, 1_000_000)
		if err := packet.writePacket(7, payload.Bytes()); err != nil {
			return err
		}
		return packet.writePacket(3, nil)
	})
	if err != nil || !result.Online || result.ModListComplete || len(result.Mods) != 0 || !strings.Contains(result.DetectionDiagnostic, "count") {
		t.Fatalf("hostile count did not fail closed at actual TCP parser: %+v err=%v", result, err)
	}
	if serverErr := <-finished; serverErr != nil && !errors.Is(serverErr, io.EOF) {
		t.Fatal(serverErr)
	}
}
