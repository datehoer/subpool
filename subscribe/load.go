package subscribe

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Source describes one subscription origin.
type Source struct {
	// Kind: "url" or "file"
	Kind string
	// Path is URL or local file path
	Path string
}

// Loaded is what one source gave at a load.
type Loaded struct {
	Forwards []string
	Err      error
	Took     time.Duration
}

// LoadAll loads all sources, auto-detects v2rayN base64 list vs Clash YAML,
// and returns unique glider forward URLs, plus what each source gave (URLs first, then files).
func LoadAll(urls, files []string) ([]string, []Loaded, error) {
	var sources []Source
	for _, u := range urls {
		sources = append(sources, Source{Kind: "url", Path: u})
	}
	for _, f := range files {
		sources = append(sources, Source{Kind: "file", Path: f})
	}
	return LoadSources(sources)
}

// LoadSources loads and converts multiple subscription sources; a failing source is skipped.
func LoadSources(sources []Source) ([]string, []Loaded, error) {
	var all []string
	var lastErr error
	each := make([]Loaded, len(sources))
	for i, s := range sources {
		start := time.Now()
		nodes, err := LoadSource(s)
		each[i] = Loaded{Forwards: nodes, Err: err, Took: time.Since(start)}
		if err != nil {
			lastErr = err
			continue
		}
		all = append(all, nodes...)
	}
	all = uniqueStrings(all)
	if len(all) == 0 {
		if lastErr != nil {
			return nil, each, lastErr
		}
		return nil, each, fmt.Errorf("no valid nodes from subscriptions")
	}
	return all, each, nil
}

// LoadSource loads one URL or file and converts to glider forwards.
func LoadSource(s Source) ([]string, error) {
	var raw string
	var err error
	switch s.Kind {
	case "url":
		raw, err = fetchURL(s.Path)
	case "file":
		b, e := os.ReadFile(s.Path)
		raw, err = string(b), e
		if err != nil {
			return nil, fmt.Errorf("无法读取文件: %v", err)
		}
	default:
		return nil, fmt.Errorf("unknown source kind %q", s.Kind)
	}
	if err != nil {
		return nil, err
	}
	return ParseAndConvert(raw)
}

// ParseAndConvert auto-detects subscription format and returns glider URLs.
func ParseAndConvert(content string) ([]string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty subscription content")
	}

	// 1) Clash YAML (plain text)
	if LooksLikeClash(content) {
		fwds, err := ParseClashYAML([]byte(content))
		if err == nil && len(fwds) > 0 {
			return fwds, nil
		}
		// clearly a Clash doc (has proxies:) — surface real error, don't pretend base64
		if strings.Contains(strings.ToLower(content), "proxies:") {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("clash yaml: parsed but no usable nodes")
		}
		// otherwise fall through — might be false positive
	}

	// 2) v2rayN base64 list or plain URI list
	lines, err := decodeSubscription(content)
	if err != nil {
		// 3) base64-encoded Clash YAML (some panels do this)
		if decoded, derr := tryBase64Decode(content); derr == nil && LooksLikeClash(decoded) {
			return ParseClashYAML([]byte(decoded))
		}
		return nil, err
	}

	var out []string
	for _, line := range lines {
		fwd, cerr := FormatConvert(line)
		if cerr != nil || fwd == "" {
			continue
		}
		out = append(out, fwd)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("不是标准的机场订阅链接文件！")
	}
	return out, nil
}

func fetchURL(url string) (string, error) {
	client := &http.Client{Timeout: 45 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("链接请求失败！%w", err)
	}
	// many panels require a UA
	req.Header.Set("User-Agent", "ClashForWindows/0.20.39")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("链接请求失败！%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("链接请求失败！HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20)) // 32MB
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
