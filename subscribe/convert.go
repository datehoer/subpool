package subscribe

import (
	"fmt"
	"strings"
)

// FormatConvert converts a single v2rayN node URI into a glider forward URL.
//
// Recovered from: github.com/nadoo/glider/proxy.FormatConvert
// which dispatches to convertSS / convertSSR / convertVmess / convertTrojan.
func FormatConvert(line string) (string, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil
	}

	scheme, _, ok := strings.Cut(line, "://")
	if !ok {
		return "", fmt.Errorf("invalid node: %s", line)
	}

	switch strings.ToLower(scheme) {
	case "ss":
		return convertSS(line)
	case "ssr":
		return convertSSR(line)
	case "vmess":
		return convertVmess(line)
	case "trojan":
		return convertTrojan(line)
	case "hy2", "hysteria2":
		// already glider-compatible URL from clash converter; pass through
		return line, nil
	case "vless", "socks", "socks5", "http", "https":
		// pass through — glider may understand some of these as-is
		return line, nil
	default:
		return "", fmt.Errorf("unsupported scheme: %s", scheme)
	}
}
