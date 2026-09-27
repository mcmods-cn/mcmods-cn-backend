package serverprobe

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxConfigurationPacketBytes          = 2 << 20
	maxFabricRegistrySyncBytes           = 8 << 20
	maxConfigurationPacketCount          = 512
	maximumConfigurationIdentifiers      = 16_384
	maximumConfigurationNamespaces       = 4_096
	maximumConfigurationNBTNodes         = 16_384
	maximumConcurrentConfigurationProbes = 4
	configurationProbeTimeout            = 30 * time.Second
	minecraft1211Protocol                = 767
)

var configurationProbeSlots = make(chan struct{}, maximumConcurrentConfigurationProbes)

var errConfigurationProbeCapacity = errors.New("configuration probe capacity is full")

type configurationParseBudget struct {
	remaining int64
}

func newConfigurationParseBudget() *configurationParseBudget {
	return &configurationParseBudget{remaining: maximumConfigurationIdentifiers}
}

func (budget *configurationParseBudget) claim(label string, count int32, remainingBytes int, minimumBytes int64) error {
	if budget == nil || count < 0 || minimumBytes <= 0 || int64(count) > budget.remaining ||
		int64(count)*minimumBytes > int64(remainingBytes) {
		return fmt.Errorf("invalid %s count %d for remaining probe budget", label, count)
	}
	budget.remaining -= int64(count)
	return nil
}

func acquireConfigurationProbeSlot(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	select {
	case configurationProbeSlots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-configurationProbeSlots }) }, nil
	default:
		return nil, errConfigurationProbeCapacity
	}
}

type configurationDiscovery struct {
	Loader     string
	Namespaces []string
	Confidence map[string]string
	Diagnostic string
}

type configurationPacketConn struct {
	conn                 net.Conn
	reader               *bufio.Reader
	compressionThreshold int
}

// configurationProtocolForStatus keeps the public status protocol intact, but
// uses the protocol implemented by this parser when a proxy reports its own
// broad compatibility range instead of the NeoForge backend version.
func configurationProtocolForStatus(reported int, versionName string) (int, bool) {
	if reported == minecraft1211Protocol {
		return reported, false
	}
	versionName = strings.ToLower(strings.TrimSpace(versionName))
	for _, proxy := range []string{"velocity", "bungeecord", "waterfall"} {
		if strings.Contains(versionName, proxy) {
			return minecraft1211Protocol, true
		}
	}
	return reported, false
}

func minecraftVersionForConfigurationProtocol(protocol int) string {
	if protocol == minecraft1211Protocol {
		return "1.21.1"
	}
	return ""
}

func probeConfigurationNamespaces(ctx context.Context, target Target, protocol int) configurationDiscovery {
	result := configurationDiscovery{Confidence: map[string]string{}}
	probeCtx, cancel := context.WithTimeout(ctx, configurationProbeTimeout)
	defer cancel()
	conn, err := (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1}).DialContext(
		probeCtx, "tcp", net.JoinHostPort(target.ConnectIP.String(), fmt.Sprint(target.ConnectPort)),
	)
	if err != nil {
		result.Diagnostic = "高版本模组探测连接失败：" + err.Error()
		return result
	}
	defer conn.Close()
	deadline := time.Now().Add(configurationProbeTimeout)
	if contextDeadline, ok := probeCtx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = conn.SetDeadline(deadline)
	packetConn := &configurationPacketConn{
		conn: conn, reader: bufio.NewReader(conn), compressionThreshold: -1,
	}
	if err = sendConfigurationHandshake(packetConn, protocol, target.HandshakeHost, uint16(target.HandshakePort)); err != nil {
		result.Diagnostic = err.Error()
		return result
	}
	if err = sendConfigurationLoginStart(packetConn); err != nil {
		result.Diagnostic = err.Error()
		return result
	}

	channelEvidence := map[string]int{}
	registryEvidence := map[string]int{}
	parseBudget := newConfigurationParseBudget()
	var fabricRegistryData []byte
	phase := "login"
	for packetNumber := 0; packetNumber < maxConfigurationPacketCount; packetNumber++ {
		packetID, payload, readErr := packetConn.readPacket()
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				result.Diagnostic = "高版本模组探测已停止：" + readErr.Error()
			}
			break
		}
		if phase == "login" {
			switch packetID {
			case 0:
				result.Diagnostic = "服务器在登录阶段拒绝了只读探测"
				packetNumber = maxConfigurationPacketCount
			case 1:
				result.Diagnostic = "服务器要求在线验证，无法进入 Configuration 阶段"
				packetNumber = maxConfigurationPacketCount
			case 2:
				if err = packetConn.writePacket(3, nil); err != nil {
					result.Diagnostic = err.Error()
					packetNumber = maxConfigurationPacketCount
					continue
				}
				phase = "configuration"
				_ = sendConfigurationCustomPayload(
					packetConn,
					"minecraft:register",
					encodeNulSeparatedChannels([]string{fabricDirectRegistryChannel}),
				)
			case 3:
				threshold, thresholdErr := readVarInt(bytes.NewReader(payload))
				if thresholdErr != nil {
					result.Diagnostic = thresholdErr.Error()
					packetNumber = maxConfigurationPacketCount
				} else {
					packetConn.compressionThreshold = int(threshold)
				}
			case 4:
				reader := bytes.NewReader(payload)
				transactionID, queryErr := readVarInt(reader)
				if queryErr != nil {
					result.Diagnostic = queryErr.Error()
					packetNumber = maxConfigurationPacketCount
					continue
				}
				response := new(bytes.Buffer)
				writeVarInt(response, transactionID)
				_ = response.WriteByte(0)
				_ = packetConn.writePacket(2, response.Bytes())
			case 5:
				reader := bytes.NewReader(payload)
				key, cookieErr := readProtocolString(reader, 32767)
				if cookieErr != nil {
					result.Diagnostic = cookieErr.Error()
					packetNumber = maxConfigurationPacketCount
					continue
				}
				response := new(bytes.Buffer)
				_ = writeProtocolString(response, key)
				_ = response.WriteByte(0)
				_ = packetConn.writePacket(4, response.Bytes())
			}
			continue
		}

		switch packetID {
		case 0:
			reader := bytes.NewReader(payload)
			key, cookieErr := readProtocolString(reader, 32767)
			if cookieErr == nil {
				response := new(bytes.Buffer)
				_ = writeProtocolString(response, key)
				_ = response.WriteByte(0)
				_ = packetConn.writePacket(1, response.Bytes())
			}
		case 1:
			reader := bytes.NewReader(payload)
			payloadID, payloadErr := readProtocolString(reader, 32767)
			if payloadErr != nil {
				result.Diagnostic = payloadErr.Error()
				continue
			}
			body := make([]byte, reader.Len())
			_, _ = io.ReadFull(reader, body)
			switch payloadID {
			case "minecraft:register", "minecraft:unregister":
				if recordErr := recordConfigurationIDs(channelEvidence, parseNulSeparatedChannels(body)); recordErr != nil {
					result.Diagnostic = recordErr.Error()
				}
			case "neoforge:register":
				result.Loader = "neoforge"
				queryReader := bytes.NewReader(body)
				if count, queryErr := readVarInt(queryReader); queryErr == nil && count == 0 {
					response := new(bytes.Buffer)
					_ = writeProtocolString(response, "neoforge:register")
					writeNeoForgeConfigurationQuery(response)
					_ = packetConn.writePacket(2, response.Bytes())
				}
			case "neoforge:network":
				result.Loader = "neoforge"
				if ids, networkErr := parseConfigurationNetworkChannels(body); networkErr == nil {
					if recordErr := recordConfigurationIDs(channelEvidence, ids); recordErr != nil {
						result.Diagnostic = recordErr.Error()
					}
				} else {
					result.Diagnostic = "NeoForge 网络能力解析失败：" + networkErr.Error()
				}
			case "neoforge:frozen_registry_sync_start":
				result.Loader = "neoforge"
				if _, registryErr := parseConfigurationResourceLocationList(body); registryErr != nil {
					result.Diagnostic = "NeoForge 注册表清单解析失败：" + registryErr.Error()
				}
			case "neoforge:frozen_registry":
				result.Loader = "neoforge"
				if registryName, entries, registryErr := parseConfigurationFrozenRegistryWithBudget(body, parseBudget); registryErr == nil {
					if recordErr := recordConfigurationIDs(registryEvidence, append(entries, registryName)); recordErr != nil {
						result.Diagnostic = recordErr.Error()
					}
				} else {
					result.Diagnostic = "NeoForge 注册表解析失败：" + registryErr.Error()
				}
			case "neoforge:frozen_registry_sync_completed":
				result.Loader = "neoforge"
				response := new(bytes.Buffer)
				_ = writeProtocolString(response, payloadID)
				_ = packetConn.writePacket(2, response.Bytes())
			case "neoforge:modded_network_setup_failed":
				result.Loader = "neoforge"
				result.Diagnostic = "已从 NeoForge 协商失败信息提取可见命名空间"
			case fabricDirectRegistryChannel:
				result.Loader = "fabric"
				if len(body) != 0 {
					if len(fabricRegistryData)+len(body) > maxFabricRegistrySyncBytes {
						result.Diagnostic = "Fabric 注册表同步数据超过安全限制"
						packetNumber = maxConfigurationPacketCount
						continue
					}
					fabricRegistryData = append(fabricRegistryData, body...)
					continue
				}
				registryNames, entries, registryErr := parseFabricRegistrySyncWithBudget(fabricRegistryData, parseBudget)
				if registryErr != nil {
					result.Diagnostic = "Fabric 注册表解析失败：" + registryErr.Error()
					continue
				}
				if recordErr := recordConfigurationIDs(registryEvidence, registryNames); recordErr != nil {
					result.Diagnostic = recordErr.Error()
					continue
				}
				if recordErr := recordConfigurationIDs(registryEvidence, entries); recordErr != nil {
					result.Diagnostic = recordErr.Error()
					continue
				}
				if err = sendConfigurationCustomPayload(packetConn, "fabric:registry/sync/complete", nil); err != nil {
					result.Diagnostic = "发送 Fabric 注册表完成回执失败：" + err.Error()
				}
			}
		case 2:
			if result.Diagnostic == "" {
				result.Diagnostic = "服务器在 Configuration 阶段结束了只读探测"
			}
			packetNumber = maxConfigurationPacketCount
		case 3:
			if len(channelEvidence) == 0 && len(registryEvidence) == 0 && result.Diagnostic == "" {
				result.Diagnostic = "服务器在 Configuration 阶段结束了只读探测"
			}
			packetNumber = maxConfigurationPacketCount
		case 4:
			if len(payload) == 8 {
				_ = packetConn.writePacket(4, payload)
			}
		case 5:
			if len(payload) == 4 {
				_ = packetConn.writePacket(5, payload)
			}
		case 7:
			registryName, entries, registryErr := parseConfigurationDynamicRegistryWithBudget(payload, parseBudget)
			if registryErr != nil {
				result.Diagnostic = "动态注册表解析失败：" + registryErr.Error()
			} else {
				if recordErr := recordConfigurationIDs(registryEvidence, append(entries, registryName)); recordErr != nil {
					result.Diagnostic = recordErr.Error()
				}
			}
		case 13:
			packetNumber = maxConfigurationPacketCount
		case 14:
			response := new(bytes.Buffer)
			writeVarInt(response, 0)
			_ = packetConn.writePacket(7, response.Bytes())
		}
	}

	allNamespaces := make(map[string]struct{}, len(channelEvidence)+len(registryEvidence))
	for namespace := range channelEvidence {
		allNamespaces[namespace] = struct{}{}
	}
	for namespace := range registryEvidence {
		allNamespaces[namespace] = struct{}{}
	}
	for namespace := range allNamespaces {
		if !isDiscoverableModNamespace(namespace) {
			continue
		}
		if channelEvidence[namespace] > 0 && registryEvidence[namespace] > 0 {
			result.Confidence[namespace] = "high"
		} else {
			result.Confidence[namespace] = "inferred"
		}
	}
	for namespace := range result.Confidence {
		result.Namespaces = append(result.Namespaces, namespace)
	}
	sort.Strings(result.Namespaces)
	if len(result.Namespaces) > 0 && result.Diagnostic == "" {
		result.Diagnostic = "高版本服务器仅能远程推断公开命名空间，结果不代表完整 JAR 列表"
	}
	return result
}

func sendConfigurationHandshake(conn *configurationPacketConn, protocol int, host string, port uint16) error {
	payload := new(bytes.Buffer)
	writeVarInt(payload, int32(protocol))
	if err := writeProtocolString(payload, host); err != nil {
		return err
	}
	_ = binary.Write(payload, binary.BigEndian, port)
	writeVarInt(payload, 2)
	return conn.writePacket(0, payload.Bytes())
}

func sendConfigurationLoginStart(conn *configurationPacketConn) error {
	payload := new(bytes.Buffer)
	username := fmt.Sprintf("MCMProbe%07d", time.Now().UnixNano()%10_000_000)
	if err := writeProtocolString(payload, username); err != nil {
		return err
	}
	uuid := make([]byte, 16)
	if _, err := rand.Read(uuid); err != nil {
		return err
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	_, _ = payload.Write(uuid)
	return conn.writePacket(0, payload.Bytes())
}

func sendConfigurationCustomPayload(conn *configurationPacketConn, payloadID string, body []byte) error {
	payload := new(bytes.Buffer)
	if err := writeProtocolString(payload, payloadID); err != nil {
		return err
	}
	_, _ = payload.Write(body)
	return conn.writePacket(2, payload.Bytes())
}

func (conn *configurationPacketConn) readPacket() (int32, []byte, error) {
	length, err := readVarInt(conn.reader)
	if err != nil {
		return 0, nil, err
	}
	if length <= 0 || length > maxConfigurationPacketBytes {
		return 0, nil, fmt.Errorf("invalid Configuration packet length %d", length)
	}
	frame := make([]byte, length)
	if _, err = io.ReadFull(conn.reader, frame); err != nil {
		return 0, nil, err
	}
	packetData := frame
	if conn.compressionThreshold >= 0 {
		frameReader := bytes.NewReader(frame)
		uncompressedLength, readErr := readVarInt(frameReader)
		if readErr != nil {
			return 0, nil, readErr
		}
		if uncompressedLength < 0 || uncompressedLength > maxConfigurationPacketBytes {
			return 0, nil, errors.New("configuration packet expands beyond limit")
		}
		if uncompressedLength == 0 {
			packetData = make([]byte, frameReader.Len())
			_, _ = io.ReadFull(frameReader, packetData)
		} else {
			zlibReader, zlibErr := zlib.NewReader(frameReader)
			if zlibErr != nil {
				return 0, nil, zlibErr
			}
			packetData, zlibErr = io.ReadAll(io.LimitReader(zlibReader, maxConfigurationPacketBytes+1))
			closeErr := zlibReader.Close()
			if zlibErr != nil {
				return 0, nil, zlibErr
			}
			if closeErr != nil {
				return 0, nil, closeErr
			}
			if len(packetData) != int(uncompressedLength) {
				return 0, nil, errors.New("configuration decompressed length mismatch")
			}
		}
	}
	reader := bytes.NewReader(packetData)
	packetID, err := readVarInt(reader)
	if err != nil {
		return 0, nil, err
	}
	payload := make([]byte, reader.Len())
	_, _ = io.ReadFull(reader, payload)
	return packetID, payload, nil
}

func (conn *configurationPacketConn) writePacket(packetID int32, payload []byte) error {
	packet := new(bytes.Buffer)
	writeVarInt(packet, packetID)
	_, _ = packet.Write(payload)
	raw := packet.Bytes()
	frame := new(bytes.Buffer)
	if conn.compressionThreshold >= 0 {
		if len(raw) >= conn.compressionThreshold {
			writeVarInt(frame, int32(len(raw)))
			zlibWriter := zlib.NewWriter(frame)
			if _, err := zlibWriter.Write(raw); err != nil {
				return err
			}
			if err := zlibWriter.Close(); err != nil {
				return err
			}
		} else {
			writeVarInt(frame, 0)
			_, _ = frame.Write(raw)
		}
	} else {
		_, _ = frame.Write(raw)
	}
	header := new(bytes.Buffer)
	writeVarInt(header, int32(frame.Len()))
	if _, err := conn.conn.Write(header.Bytes()); err != nil {
		return err
	}
	_, err := conn.conn.Write(frame.Bytes())
	return err
}

func writeNeoForgeConfigurationQuery(writer io.Writer) {
	clientbound := int32(1)
	type component struct {
		id   string
		flow *int32
	}
	components := []component{
		{id: "neoforge:frozen_registry_sync_start", flow: &clientbound},
		{id: "neoforge:frozen_registry", flow: &clientbound},
		{id: "neoforge:frozen_registry_sync_completed"},
	}
	writeVarInt(writer, 1)
	writeVarInt(writer, 4)
	writeVarInt(writer, int32(len(components)))
	for _, component := range components {
		_ = writeProtocolString(writer, component.id)
		_ = writeProtocolString(writer, "1")
		if component.flow == nil {
			_, _ = writer.Write([]byte{0})
		} else {
			_, _ = writer.Write([]byte{1})
			writeVarInt(writer, *component.flow)
		}
		_, _ = writer.Write([]byte{1})
	}
}

func parseConfigurationNetworkChannels(data []byte) ([]string, error) {
	reader := bytes.NewReader(data)
	protocolCount, err := readVarInt(reader)
	if err != nil || protocolCount < 0 || protocolCount > 5 {
		return nil, errors.New("invalid protocol map count")
	}
	var ids []string
	for protocolIndex := int32(0); protocolIndex < protocolCount; protocolIndex++ {
		if _, err = readVarInt(reader); err != nil {
			return nil, err
		}
		channelCount, countErr := readVarInt(reader)
		if countErr != nil || channelCount < 0 || channelCount > 4096 ||
			len(ids)+int(channelCount) > maximumConfigurationIdentifiers || int64(channelCount)*3 > int64(reader.Len()) {
			return nil, errors.New("invalid channel count")
		}
		for channelIndex := int32(0); channelIndex < channelCount; channelIndex++ {
			mapKey, keyErr := readProtocolString(reader, 32767)
			if keyErr != nil {
				return nil, keyErr
			}
			channelID, idErr := readProtocolString(reader, 32767)
			if idErr != nil {
				return nil, idErr
			}
			if _, err = readProtocolString(reader, 32767); err != nil {
				return nil, err
			}
			if mapKey != channelID {
				return nil, errors.New("network channel key does not match its ID")
			}
			ids = append(ids, channelID)
		}
	}
	if reader.Len() != 0 {
		return nil, errors.New("network setup contains trailing data")
	}
	return ids, nil
}

func parseConfigurationFrozenRegistry(data []byte) (string, []string, error) {
	return parseConfigurationFrozenRegistryWithBudget(data, newConfigurationParseBudget())
}

func parseConfigurationFrozenRegistryWithBudget(data []byte, budget *configurationParseBudget) (string, []string, error) {
	reader := bytes.NewReader(data)
	registryName, err := readProtocolString(reader, 32767)
	if err != nil {
		return "", nil, err
	}
	if err = budget.claim("frozen registry name", 1, reader.Len(), 1); err != nil {
		return "", nil, err
	}
	count, err := readVarInt(reader)
	if err != nil || budget.claim("frozen registry entry", count, reader.Len(), 2) != nil {
		return "", nil, errors.New("invalid frozen registry entry count")
	}
	entries := make([]string, 0, min(int(count), 256))
	for index := int32(0); index < count; index++ {
		if _, err = readVarInt(reader); err != nil {
			return "", nil, err
		}
		id, idErr := readProtocolString(reader, 32767)
		if idErr != nil {
			return "", nil, idErr
		}
		entries = append(entries, id)
	}
	aliasCount, err := readVarInt(reader)
	if err != nil || aliasCount < 0 || aliasCount > maximumConfigurationIdentifiers/2 ||
		budget.claim("frozen registry alias", aliasCount*2, reader.Len(), 1) != nil {
		return "", nil, errors.New("invalid frozen registry alias count")
	}
	for index := int32(0); index < aliasCount; index++ {
		from, fromErr := readProtocolString(reader, 32767)
		if fromErr != nil {
			return "", nil, fromErr
		}
		to, toErr := readProtocolString(reader, 32767)
		if toErr != nil {
			return "", nil, toErr
		}
		entries = append(entries, from, to)
	}
	if reader.Len() != 0 {
		return "", nil, fmt.Errorf("%d trailing bytes in frozen registry", reader.Len())
	}
	return registryName, entries, nil
}

func parseNulSeparatedChannels(data []byte) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, min(64, maximumConfigurationIdentifiers))
	for len(data) > 0 && len(result) < maximumConfigurationIdentifiers {
		end := bytes.IndexByte(data, 0)
		if end < 0 {
			end = len(data)
		}
		value := data[:end]
		trimmed := strings.TrimSpace(string(value))
		if trimmed != "" {
			if _, exists := seen[trimmed]; !exists {
				seen[trimmed] = struct{}{}
				result = append(result, trimmed)
			}
		}
		if end == len(data) {
			break
		}
		data = data[end+1:]
	}
	return result
}

func encodeNulSeparatedChannels(channels []string) []byte {
	return []byte(strings.Join(parseNulSeparatedChannels([]byte(strings.Join(channels, "\x00"))), "\x00"))
}

func parseConfigurationResourceLocationList(data []byte) ([]string, error) {
	reader := bytes.NewReader(data)
	count, err := readVarInt(reader)
	if err != nil || count < 0 || count > 4096 || int64(count) > int64(reader.Len()) {
		return nil, errors.New("invalid resource location count")
	}
	values := make([]string, 0, min(int(count), 256))
	for index := int32(0); index < count; index++ {
		value, readErr := readProtocolString(reader, 32767)
		if readErr != nil {
			return nil, readErr
		}
		values = append(values, value)
	}
	if reader.Len() != 0 {
		return nil, fmt.Errorf("%d trailing resource location bytes", reader.Len())
	}
	return values, nil
}

func recordConfigurationIDs(target map[string]int, ids []string) error {
	for _, id := range ids {
		if namespace, _, ok := strings.Cut(strings.ToLower(id), ":"); ok {
			if _, exists := target[namespace]; !exists && len(target) >= maximumConfigurationNamespaces {
				return errors.New("configuration namespace evidence exceeds safety limit")
			}
			target[namespace]++
		}
	}
	return nil
}

func isDiscoverableModNamespace(namespace string) bool {
	switch namespace {
	case "", "minecraft", "c", "forge", "neoforge", "fabric", "fabric-api", "fabricloader":
		return false
	default:
		return normalizeModID(namespace) != ""
	}
}
