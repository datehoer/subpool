package subscribe

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// VmessInfo is the v2rayN share-link JSON object.
// Field tags follow the v2rayN share-link JSON schema.
type VmessInfo struct {
	V    string      `json:"v"`
	Ps   string      `json:"ps"`
	Add  string      `json:"add"`
	Port interface{} `json:"port"` // string or number
	ID   string      `json:"id"`
	Aid  interface{} `json:"aid"` // string or number
	Scy  string      `json:"scy"`
	Net  string      `json:"net"`
	Type string      `json:"type"`
	Host string      `json:"host"`
	Path string      `json:"path"`
	TLS  string      `json:"tls"`
	SNI  string      `json:"sni"`
	Alpn string      `json:"alpn"`
}

// convertVmess recovered from proxy.convertVmess.
// Produces glider chain URLs matching these format strings from the binary:
//
//	wss://%s:%s%s?host=%s&skipVerify=true,vmess://%s@alterID=%s
//	ws://%s:%s%s?host=%s,vmess://%s@alterID=%s
//	tls://%s:%s?serverName=%s&skipVerify=true,vmess://%s@alterID=%s
//	vmess://%s@%s:%s%s?alterID=%s
func convertVmess(line string) (string, error) {
	raw := strings.TrimPrefix(line, "vmess://")
	raw = strings.TrimPrefix(raw, "VMESS://")

	data, err := tryBase64DecodeBytes(raw)
	if err != nil {
		return "", fmt.Errorf("decode vmess err: %s", err)
	}

	var info VmessInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return "", fmt.Errorf("json unmarshal err: %s", err)
	}

	port := anyToString(info.Port)
	aid := anyToString(info.Aid)
	if aid == "" {
		aid = "0"
	}
	path := normalizePath(info.Path)
	hostHeader := info.Host
	if hostHeader == "" {
		hostHeader = info.Add
	}
	sni := info.SNI
	if sni == "" {
		sni = hostHeader
	}

	net := strings.ToLower(info.Net)
	tlsOn := strings.EqualFold(info.TLS, "tls") || strings.EqualFold(info.TLS, "xtls")

	switch {
	case net == "ws" && tlsOn:
		// wss://host:port/path?host=HOST&skipVerify=true,vmess://uuid@alterID=aid
		return fmt.Sprintf("wss://%s:%s%s?host=%s&skipVerify=true,vmess://%s@alterID=%s",
			info.Add, port, path, hostHeader, info.ID, aid), nil
	case net == "ws":
		return fmt.Sprintf("ws://%s:%s%s?host=%s,vmess://%s@alterID=%s",
			info.Add, port, path, hostHeader, info.ID, aid), nil
	case tlsOn && net != "ws":
		return fmt.Sprintf("tls://%s:%s?serverName=%s&skipVerify=true,vmess://%s@alterID=%s",
			info.Add, port, sni, info.ID, aid), nil
	default:
		// tcp / plain
		return fmt.Sprintf("vmess://%s@%s:%s%s?alterID=%s",
			info.ID, info.Add, port, path, aid), nil
	}
}

func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

func anyToString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		// json numbers
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func tryBase64DecodeBytes(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			return -1
		}
		return r
	}, s)
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.RawURLEncoding,
	}
	for _, c := range []string{s, padBase64(s)} {
		for _, enc := range encodings {
			if b, err := enc.DecodeString(c); err == nil && len(b) > 0 {
				return b, nil
			}
		}
	}
	return nil, fmt.Errorf("base64 decode failed")
}
