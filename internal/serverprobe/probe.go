package serverprobe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultMinecraftPort = 25565
	maxStatusPacketBytes = 2 << 20
)

type Mod struct {
	ID         string `json:"id"`
	Version    string `json:"version,omitempty"`
	Source     string `json:"source"`
	Confidence string `json:"confidence"`
}

type Result struct {
	Address                  string `json:"address"`
	NormalizedAddress        string `json:"normalizedAddress"`
	HandshakeHost            string `json:"handshakeHost"`
	ConnectHost              string `json:"connectHost"`
	ConnectPort              int    `json:"connectPort"`
	Online                   bool   `json:"online"`
	LatencyMS                int    `json:"latencyMs"`
	PlayersOnline            int    `json:"playersOnline"`
	PlayersMax               int    `json:"playersMax"`
	MOTD                     string `json:"motd"`
	MinecraftVersion         string `json:"minecraftVersion"`
	DetectedMinecraftVersion string `json:"detectedMinecraftVersion,omitempty"`
	Protocol                 int    `json:"protocol"`
	IconDataURI              string `json:"iconDataUri,omitempty"`
	Modded                   bool   `json:"modded"`
	Loader                   string `json:"loader,omitempty"`
	ModListComplete          bool   `json:"modListComplete"`
	Mods                     []Mod  `json:"mods"`
	DetectionDiagnostic      string `json:"detectionDiagnostic,omitempty"`
}

type Target struct {
	Address           string
	NormalizedAddress string
	HandshakeHost     string
	HandshakePort     int
	ConnectHost       string
	ConnectPort       int
	ConnectIP         net.IP
}

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
	LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error)
}

// Probe performs a Minecraft Java Server List Ping. It deliberately dials the
// already validated IP rather than resolving the hostname a second time, which
// prevents DNS rebinding from bypassing the SSRF guard.
func Probe(ctx context.Context, address string) (Result, error) {
	return ProbeWithOptions(ctx, address, false)
}

// ProbeWithOptions optionally enters the modern Configuration phase after a
// successful status ping. That active login probe is only used by the explicit
// submission wizard because it can appear in a server log; scheduled health
// checks stay on the passive Server List Ping path.
func ProbeWithOptions(ctx context.Context, address string, discoverNamespaces bool) (Result, error) {
	target, err := ResolveTarget(ctx, net.DefaultResolver, address)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Address:           target.Address,
		NormalizedAddress: target.NormalizedAddress,
		HandshakeHost:     target.HandshakeHost,
		ConnectHost:       target.ConnectHost,
		ConnectPort:       target.ConnectPort,
		Mods:              []Mod{},
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	dialer := net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1}
	started := time.Now()
	conn, err := dialer.DialContext(deadlineCtx, "tcp", net.JoinHostPort(target.ConnectIP.String(), strconv.Itoa(target.ConnectPort)))
	if err != nil {
		return result, fmt.Errorf("connect Minecraft server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))

	if err = writeStatusHandshake(conn, target.HandshakeHost, target.HandshakePort); err != nil {
		return result, err
	}
	if err = writePacket(conn, 0, nil); err != nil {
		return result, fmt.Errorf("send status request: %w", err)
	}
	reader := bufio.NewReader(conn)
	packetLength, err := readVarInt(reader)
	if err != nil {
		return result, fmt.Errorf("read status packet length: %w", err)
	}
	if packetLength <= 0 || packetLength > maxStatusPacketBytes {
		return result, fmt.Errorf("invalid status packet size %d", packetLength)
	}
	packet := make([]byte, packetLength)
	if _, err = io.ReadFull(reader, packet); err != nil {
		return result, fmt.Errorf("read status packet: %w", err)
	}
	packetReader := bytes.NewReader(packet)
	packetID, err := readVarInt(packetReader)
	if err != nil || packetID != 0 {
		return result, fmt.Errorf("unexpected status packet id %d", packetID)
	}
	rawStatus, err := readProtocolString(packetReader, maxStatusPacketBytes)
	if err != nil {
		return result, fmt.Errorf("read status JSON: %w", err)
	}
	if packetReader.Len() != 0 {
		return result, errors.New("status packet contains trailing data")
	}
	result.LatencyMS = max(0, int(time.Since(started).Milliseconds()))
	if err = parseStatusJSON([]byte(rawStatus), &result); err != nil {
		return result, err
	}
	result.Online = true
	if discoverNamespaces && len(result.Mods) == 0 && result.Protocol >= 764 {
		discoveryProtocol, proxyProtocolFallback := configurationProtocolForStatus(
			result.Protocol,
			result.MinecraftVersion,
		)
		discovery := probeConfigurationNamespaces(ctx, target, discoveryProtocol)
		if discovery.Loader != "" {
			result.Loader = discovery.Loader
			result.Modded = true
			result.DetectedMinecraftVersion = minecraftVersionForConfigurationProtocol(discoveryProtocol)
		}
		for _, namespace := range discovery.Namespaces {
			result.Mods = append(result.Mods, Mod{
				ID: namespace, Source: "configuration", Confidence: discovery.Confidence[namespace],
			})
		}
		if discovery.Diagnostic != "" {
			result.DetectionDiagnostic = discovery.Diagnostic
		}
		if proxyProtocolFallback {
			note := fmt.Sprintf(
				"服务器代理报告协议 %d；Configuration 模组探测已改用 Minecraft 1.21.1 协议 %d",
				result.Protocol,
				discoveryProtocol,
			)
			if result.DetectionDiagnostic == "" {
				result.DetectionDiagnostic = note
			} else {
				result.DetectionDiagnostic = note + "。" + result.DetectionDiagnostic
			}
		}
	}
	return result, nil
}

func ResolveTarget(ctx context.Context, resolver Resolver, address string) (Target, error) {
	host, port, explicitPort, normalized, err := ParseAddress(address)
	if err != nil {
		return Target{}, err
	}
	target := Target{
		Address:           strings.TrimSpace(address),
		NormalizedAddress: normalized,
		HandshakeHost:     host,
		HandshakePort:     port,
		ConnectHost:       host,
		ConnectPort:       port,
	}
	if !explicitPort && net.ParseIP(host) == nil {
		if _, records, lookupErr := resolver.LookupSRV(ctx, "minecraft", "tcp", host); lookupErr == nil && len(records) > 0 {
			target.ConnectHost = strings.TrimSuffix(strings.ToLower(records[0].Target), ".")
			target.ConnectPort = int(records[0].Port)
		}
	}
	addresses, err := resolver.LookupIPAddr(ctx, target.ConnectHost)
	if err != nil || len(addresses) == 0 {
		if err == nil {
			err = errors.New("host has no IP address")
		}
		return Target{}, fmt.Errorf("resolve Minecraft server: %w", err)
	}
	for _, addressRow := range addresses {
		if !isPublicServerIP(addressRow.IP) {
			return Target{}, fmt.Errorf("server address resolves to a private or reserved IP")
		}
	}
	target.ConnectIP = addresses[0].IP
	return target, nil
}

func ParseAddress(value string) (host string, port int, explicitPort bool, normalized string, err error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 255 {
		return "", 0, false, "", errors.New("server address is empty or too long")
	}
	value = strings.TrimPrefix(value, "minecraft://")
	value = strings.TrimPrefix(value, "MINECRAFT://")
	value = strings.TrimSuffix(value, "/")
	if strings.ContainsAny(value, "/?#@ \t\r\n") {
		return "", 0, false, "", errors.New("server address must only contain a host and optional port")
	}

	port = defaultMinecraftPort
	switch {
	case strings.HasPrefix(value, "["):
		end := strings.Index(value, "]")
		if end < 0 {
			return "", 0, false, "", errors.New("invalid IPv6 address")
		}
		host = value[1:end]
		if end+1 < len(value) {
			if value[end+1] != ':' {
				return "", 0, false, "", errors.New("invalid server port")
			}
			parsed, parseErr := strconv.Atoi(value[end+2:])
			if parseErr != nil {
				return "", 0, false, "", errors.New("invalid server port")
			}
			port, explicitPort = parsed, true
		}
	case strings.Count(value, ":") == 1:
		hostPart, portPart, splitErr := net.SplitHostPort(value)
		if splitErr != nil {
			return "", 0, false, "", errors.New("invalid server address")
		}
		parsed, parseErr := strconv.Atoi(portPart)
		if parseErr != nil {
			return "", 0, false, "", errors.New("invalid server port")
		}
		host, port, explicitPort = hostPart, parsed, true
	case strings.Count(value, ":") > 1:
		return "", 0, false, "", errors.New("IPv6 addresses must be enclosed in brackets")
	default:
		host = value
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || port < 1 || port > 65535 {
		return "", 0, false, "", errors.New("invalid server host or port")
	}
	if net.ParseIP(host) == nil {
		for _, label := range strings.Split(host, ".") {
			if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return "", 0, false, "", errors.New("invalid server hostname")
			}
			for _, character := range label {
				if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' {
					return "", 0, false, "", errors.New("invalid server hostname")
				}
			}
		}
	}
	normalized = net.JoinHostPort(host, strconv.Itoa(port))
	return host, port, explicitPort, normalized, nil
}

func isPublicServerIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 0 || v4[0] == 100 && v4[1]&0xc0 == 64 ||
			v4[0] == 192 && v4[1] == 0 && (v4[2] == 0 || v4[2] == 2) ||
			v4[0] == 198 && (v4[1] == 18 || v4[1] == 19 || v4[1] == 51 && v4[2] == 100) ||
			v4[0] == 203 && v4[1] == 0 && v4[2] == 113 ||
			v4[0] >= 224 {
			return false
		}
		return true
	}
	// Documentation (2001:db8::/32), unique local, and IPv4-mapped addresses.
	return !(len(ip) == net.IPv6len && ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8)
}

func parseStatusJSON(payload []byte, result *Result) error {
	var status struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct {
			Max    int `json:"max"`
			Online int `json:"online"`
		} `json:"players"`
		Description json.RawMessage `json:"description"`
		Favicon     string          `json:"favicon"`
		IsModded    bool            `json:"isModded"`
	}
	if err := json.Unmarshal(payload, &status); err != nil {
		return fmt.Errorf("decode status response: %w", err)
	}
	result.MinecraftVersion = strings.TrimSpace(status.Version.Name)
	result.Protocol = status.Version.Protocol
	result.PlayersMax = max(0, status.Players.Max)
	result.PlayersOnline = max(0, status.Players.Online)
	result.MOTD = flattenChatComponent(status.Description)
	result.IconDataURI = safeFavicon(status.Favicon)
	result.Modded = status.IsModded

	forge, err := summarizeForgeStatus(payload)
	if err != nil {
		result.DetectionDiagnostic = err.Error()
		return nil
	}
	if forge.Detected {
		result.Modded = true
		result.Loader = "forge"
		// Some proxies expose an empty forgeData object as a compatibility
		// marker. It proves that the route is modded, but it is not a complete
		// empty Mod list and must not suppress the active Configuration probe.
		result.ModListComplete = len(forge.Mods) > 0 && !forge.Truncated
		for _, mod := range forge.Mods {
			id := normalizeModID(mod.ModID)
			if id == "" || isLoaderNamespace(id) {
				continue
			}
			result.Mods = append(result.Mods, Mod{
				ID: id, Version: mod.Marker, Source: "forge_status", Confidence: "exact",
			})
		}
	}
	return nil
}

func flattenChatComponent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	var parts []string
	var visit func(any)
	visit = func(component any) {
		switch row := component.(type) {
		case string:
			parts = append(parts, row)
		case []any:
			for _, child := range row {
				visit(child)
			}
		case map[string]any:
			if text, ok := row["text"].(string); ok {
				parts = append(parts, text)
			}
			if translate, ok := row["translate"].(string); ok && len(parts) == 0 {
				parts = append(parts, translate)
			}
			if extra, ok := row["extra"]; ok {
				visit(extra)
			}
		}
	}
	visit(value)
	return strings.TrimSpace(strings.Join(parts, ""))
}

func safeFavicon(value string) string {
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(value, prefix) || len(value) > 1024*1024 {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil || len(decoded) == 0 || len(decoded) > 768*1024 ||
		!bytes.HasPrefix(decoded, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return ""
	}
	config, err := png.DecodeConfig(bytes.NewReader(decoded))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 512 || config.Height > 512 {
		return ""
	}
	return value
}

func writeStatusHandshake(writer io.Writer, host string, port int) error {
	payload := new(bytes.Buffer)
	writeVarInt(payload, -1)
	if err := writeProtocolString(payload, host); err != nil {
		return err
	}
	if err := payload.WriteByte(byte(port >> 8)); err != nil {
		return err
	}
	if err := payload.WriteByte(byte(port)); err != nil {
		return err
	}
	writeVarInt(payload, 1)
	return writePacket(writer, 0, payload.Bytes())
}

func writePacket(writer io.Writer, packetID int32, payload []byte) error {
	packet := new(bytes.Buffer)
	writeVarInt(packet, packetID)
	_, _ = packet.Write(payload)
	frame := new(bytes.Buffer)
	writeVarInt(frame, int32(packet.Len()))
	_, _ = frame.Write(packet.Bytes())
	_, err := writer.Write(frame.Bytes())
	return err
}

func writeProtocolString(writer io.Writer, value string) error {
	if !utf8.ValidString(value) || len(value) > 32767 {
		return errors.New("invalid protocol string")
	}
	writeVarInt(writer, int32(len(value)))
	_, err := io.WriteString(writer, value)
	return err
}

func readProtocolString(reader interface {
	io.Reader
	io.ByteReader
}, maximum int32) (string, error) {
	length, err := readVarInt(reader)
	if err != nil {
		return "", err
	}
	if length < 0 || length > maximum {
		return "", fmt.Errorf("invalid string length %d", length)
	}
	value := make([]byte, length)
	if _, err = io.ReadFull(reader, value); err != nil {
		return "", err
	}
	if !utf8.Valid(value) {
		return "", errors.New("invalid UTF-8 string")
	}
	return string(value), nil
}

func writeVarInt(writer io.Writer, value int32) {
	unsigned := uint32(value)
	for {
		current := byte(unsigned & 0x7f)
		unsigned >>= 7
		if unsigned != 0 {
			current |= 0x80
		}
		_, _ = writer.Write([]byte{current})
		if unsigned == 0 {
			return
		}
	}
}

func readVarInt(reader io.ByteReader) (int32, error) {
	var value uint32
	for position := uint(0); position < 35; position += 7 {
		current, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		value |= uint32(current&0x7f) << position
		if current&0x80 == 0 {
			return int32(value), nil
		}
	}
	return 0, errors.New("VarInt is too large")
}

func normalizeModID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') &&
			character != '_' && character != '-' && character != '.' {
			return ""
		}
	}
	return value
}

func isLoaderNamespace(value string) bool {
	switch value {
	case "minecraft", "forge", "neoforge", "fabricloader", "fabric-api":
		return true
	default:
		return false
	}
}
