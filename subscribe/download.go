package subscribe

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DownloadAndDecode fetches a v2rayN-style subscription URL, base64-decodes it,
// and returns one node URI per line.
//
// DownloadAndDecode fetches and decodes a subscription body into node lines.
func DownloadAndDecode(url string) ([]string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("链接请求失败！%w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return decodeSubscription(string(body))
}

// ReadAndDecodeBase64File reads a local subscription file and base64-decodes it.
//
// Recovered from: github.com/nadoo/glider/proxy.ReadAndDecodeBase64File.
func ReadAndDecodeBase64File(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// original: log.Fatalf("无法读取文件: %v", err)
		return nil, fmt.Errorf("无法读取文件: %v", err)
	}
	nodes, err := decodeSubscription(string(data))
	if err != nil {
		// original also had: 无法解码Base64数据
		return nil, err
	}
	return nodes, nil
}

func decodeSubscription(content string) ([]string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("不是标准的机场订阅链接文件！")
	}

	decoded, err := tryBase64Decode(content)
	if err != nil {
		// some providers return plain multi-line node lists
		if looksLikeNodeList(content) {
			decoded = content
		} else {
			return nil, fmt.Errorf("不是标准的机场订阅链接文件！")
		}
	}

	var out []string
	for _, line := range strings.Split(decoded, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// strip leftover CR
		line = strings.TrimSuffix(line, "\r")
		if looksLikeNode(line) {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("不是标准的机场订阅链接文件！")
	}
	return out, nil
}

func tryBase64Decode(s string) (string, error) {
	// strip whitespace that some panels insert
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
	// also try with padding fixed
	candidates := []string{s, padBase64(s)}
	var last error
	for _, c := range candidates {
		for _, enc := range encodings {
			b, err := enc.DecodeString(c)
			if err != nil {
				last = err
				continue
			}
			if len(b) == 0 {
				continue
			}
			return string(b), nil
		}
	}
	if last == nil {
		last = fmt.Errorf("无法解码Base64数据")
	}
	return "", last
}

func padBase64(s string) string {
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	return s
}

func looksLikeNodeList(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		if looksLikeNode(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func looksLikeNode(line string) bool {
	prefixes := []string{
		"vmess://", "vless://", "ss://", "ssr://", "trojan://",
		"socks://", "socks5://", "http://", "https://",
	}
	low := strings.ToLower(line)
	for _, p := range prefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}
