package subscribe

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ClashFile is the minimal Clash config we care about (proxies only).
type ClashFile struct {
	Proxies []ClashProxy `yaml:"proxies"`
}

// ClashProxy is a flexible Clash proxy entry (flow or block style).
// Extra fields are ignored by yaml.v3 when not declared.
type ClashProxy struct {
	Name           string                 `yaml:"name"`
	Type           string                 `yaml:"type"`
	Server         string                 `yaml:"server"`
	Port           interface{}            `yaml:"port"`
	UUID           string                 `yaml:"uuid"`
	AlterID        interface{}            `yaml:"alterId"`
	Cipher         string                 `yaml:"cipher"`
	Password       string                 `yaml:"password"`
	TLS            interface{}            `yaml:"tls"`
	SkipCertVerify interface{}            `yaml:"skip-cert-verify"`
	Network        string                 `yaml:"network"`
	ServerName     string                 `yaml:"servername"`
	SNI            string                 `yaml:"sni"`
	UDP            interface{}            `yaml:"udp"`
	Plugin         string                 `yaml:"plugin"`
	PluginOpts     map[string]interface{} `yaml:"plugin-opts"`
	// WebSocket (modern + legacy)
	WSOpts    map[string]interface{} `yaml:"ws-opts"`
	WSPath    string                 `yaml:"ws-path"`
	WSHeaders map[string]string      `yaml:"ws-headers"`
	// HTTP camouflage (vmess network: http)
	HTTPOpts map[string]interface{} `yaml:"http-opts"`
	// Reality / advanced (mostly skipped if unsupported)
	Flow     string `yaml:"flow"`
	ClientFP string `yaml:"client-fingerprint"`
	// SSR / Hy2 shared-ish fields
	Protocol      string `yaml:"protocol"`
	ProtocolParam string `yaml:"protocol-param"`
	Obfs          string `yaml:"obfs"` // ssr: http_simple…; hy2: salamander
	ObfsParam     string `yaml:"obfs-param"`
	ObfsPassword  string `yaml:"obfs-password"` // hy2 salamander password
	Up            string `yaml:"up"`            // hy2 bandwidth
	Down          string `yaml:"down"`
	// misc
	Username string `yaml:"username"`
}

// ParseClashYAML parses Clash config YAML and returns glider forward URLs.
func ParseClashYAML(content []byte) ([]string, error) {
	var cf ClashFile
	if err := yaml.Unmarshal(content, &cf); err != nil {
		return nil, fmt.Errorf("clash yaml: %w", err)
	}
	if len(cf.Proxies) == 0 {
		return nil, fmt.Errorf("clash yaml: no proxies")
	}

	var out []string
	skipped := map[string]int{}
	for _, p := range cf.Proxies {
		fwd, err := ClashProxyToForward(p)
		if err != nil {
			key := strings.ToLower(strings.TrimSpace(p.Type))
			if key == "" {
				key = "unknown"
			}
			// group by type + short reason
			reason := key
			if strings.Contains(err.Error(), "unsupported") {
				reason = key + "(unsupported)"
			} else if strings.Contains(err.Error(), "plugin") {
				reason = key + "(plugin)"
			}
			skipped[reason]++
			continue
		}
		if fwd != "" {
			out = append(out, fwd)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("clash yaml: no convertible proxies (total %d, skipped %v)", len(cf.Proxies), skipped)
	}
	if len(skipped) > 0 {
		// non-fatal; caller can log
		_ = skipped
	}
	return out, nil
}

// LooksLikeClash returns true if content appears to be Clash YAML.
func LooksLikeClash(content string) bool {
	s := strings.TrimSpace(content)
	if s == "" {
		return false
	}
	// quick reject base64-only blobs
	if !strings.Contains(s, "\n") && !strings.Contains(s, "proxies:") {
		return false
	}
	low := strings.ToLower(s)
	if strings.Contains(low, "proxies:") {
		return true
	}
	// single proxy list without top key is rare; still try keywords
	return strings.Contains(low, "\ntype: vmess") ||
		strings.Contains(low, "\ntype: ss") ||
		strings.Contains(low, "type: trojan") ||
		strings.Contains(low, "mixed-port:") ||
		strings.Contains(low, "proxy-groups:")
}

// ClashProxyToForward converts one Clash proxy to a glider forward URL.
func ClashProxyToForward(p ClashProxy) (string, error) {
	t := strings.ToLower(strings.TrimSpace(p.Type))
	if t == "" || p.Server == "" {
		return "", fmt.Errorf("invalid proxy %q", p.Name)
	}
	// skip placeholders / non-proxy entries
	if p.Server == "127.0.0.1" && anyToString(p.Port) == "53" {
		// keep them — user may want; but often dead. Still convert.
	}

	switch t {
	case "ss", "shadowsocks":
		return clashSS(p)
	case "ssr", "shadowsocksr":
		return clashSSR(p)
	case "vmess":
		return clashVmess(p)
	case "vless":
		return clashVless(p)
	case "trojan":
		return clashTrojan(p)
	case "http", "https":
		return clashHTTP(p)
	case "socks", "socks5", "socks5h":
		return clashSocks(p)
	case "hysteria2", "hy2":
		return clashHy2(p)
	case "hysteria", "tuic", "wireguard", "snell", "ssh":
		return "", fmt.Errorf("unsupported clash type %s (%s)", t, p.Name)
	default:
		return "", fmt.Errorf("unknown clash type %s (%s)", t, p.Name)
	}
}

func clashHy2(p ClashProxy) (string, error) {
	pass := p.Password
	if pass == "" {
		return "", fmt.Errorf("hy2 missing password: %s", p.Name)
	}
	hostport := joinHostPort(p.Server, p.Port)
	sni := firstNonEmpty(p.SNI, p.ServerName, p.Server)

	u := &url.URL{
		Scheme: "hy2",
		User:   url.User(pass),
		Host:   hostport,
	}
	q := url.Values{}
	if sni != "" {
		q.Set("sni", sni)
	}
	q.Set("skipVerify", skipVerifyStr(p))

	if strings.EqualFold(strings.TrimSpace(p.Obfs), "salamander") {
		q.Set("obfs", "salamander")
		if p.ObfsPassword != "" {
			q.Set("obfsPassword", p.ObfsPassword)
		}
	}
	if p.Up != "" {
		q.Set("up", p.Up)
	}
	if p.Down != "" {
		q.Set("down", p.Down)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func clashSS(p ClashProxy) (string, error) {
	cipher := p.Cipher
	if cipher == "" {
		cipher = "aes-256-gcm"
	}
	pass := p.Password
	if pass == "" {
		return "", fmt.Errorf("ss missing password: %s", p.Name)
	}

	plugin := strings.ToLower(strings.TrimSpace(p.Plugin))
	if plugin == "" {
		return fmt.Sprintf("ss://%s:%s@%s", cipher, urlUserInfoEscape(pass), joinHostPort(p.Server, p.Port)), nil
	}

	switch plugin {
	case "obfs", "simple-obfs", "simple_obfs":
		return clashSSSimpleObfs(p, cipher, pass)
	default:
		return "", fmt.Errorf("ss plugin not supported: %s", p.Plugin)
	}
}

func clashSSSimpleObfs(p ClashProxy, cipher, pass string) (string, error) {
	mode := strings.ToLower(pluginOpt(p.PluginOpts, "mode"))
	if mode == "" {
		mode = "http"
	}
	if mode != "http" && mode != "tls" {
		return "", fmt.Errorf("ss plugin obfs mode not supported: %s", mode)
	}

	host := pluginOpt(p.PluginOpts, "host")
	if host == "" {
		host = p.Server
	}

	transport := &url.URL{
		Scheme: "simple-obfs",
		Host:   joinHostPort(p.Server, p.Port),
	}
	q := url.Values{}
	q.Set("type", mode)
	q.Set("host", host)
	if uri := pluginOpt(p.PluginOpts, "uri"); uri != "" {
		q.Set("uri", normalizePath(uri))
	}
	if ua := pluginOpt(p.PluginOpts, "ua"); ua != "" {
		q.Set("ua", ua)
	}
	transport.RawQuery = q.Encode()

	ss := fmt.Sprintf("ss://%s:%s@", cipher, urlUserInfoEscape(pass))
	return transport.String() + "," + ss, nil
}

func pluginOpt(opts map[string]interface{}, key string) string {
	for k, v := range opts {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(anyToString(v))
		}
	}
	return ""
}

func clashSSR(p ClashProxy) (string, error) {
	cipher := p.Cipher
	if cipher == "" {
		return "", fmt.Errorf("ssr missing cipher")
	}
	pass := p.Password
	proto := p.Protocol
	if proto == "" {
		proto = "origin"
	}
	obfs := p.Obfs
	if obfs == "" {
		obfs = "plain"
	}
	return fmt.Sprintf(
		"ssr://%s:%s@%s?protocol=%s&protocol_param=%s&obfs=%s&obfs_param=%s",
		cipher, urlUserInfoEscape(pass), joinHostPort(p.Server, p.Port),
		proto, p.ProtocolParam, obfs, p.ObfsParam,
	), nil
}

func clashVmess(p ClashProxy) (string, error) {
	if p.UUID == "" {
		return "", fmt.Errorf("vmess missing uuid: %s", p.Name)
	}
	aid := anyToString(p.AlterID)
	if aid == "" {
		aid = "0"
	}
	hostHeader, path := clashWSMeta(p)
	tlsOn := asBool(p.TLS)
	net := strings.ToLower(p.Network)
	if net == "" {
		net = "tcp"
	}
	server := p.Server
	port := anyToString(p.Port)

	switch {
	case net == "ws" && tlsOn:
		return fmt.Sprintf("wss://%s:%s%s?host=%s&skipVerify=%s,vmess://%s@alterID=%s",
			server, port, path, hostHeader, skipVerifyStr(p), p.UUID, aid), nil
	case net == "ws":
		return fmt.Sprintf("ws://%s:%s%s?host=%s,vmess://%s@alterID=%s",
			server, port, path, hostHeader, p.UUID, aid), nil
	case tlsOn:
		sni := firstNonEmpty(p.ServerName, p.SNI, hostHeader, server)
		return fmt.Sprintf("tls://%s:%s?serverName=%s&skipVerify=%s,vmess://%s@alterID=%s",
			server, port, sni, skipVerifyStr(p), p.UUID, aid), nil
	default:
		// tcp / http camouflage — glider plain vmess
		return fmt.Sprintf("vmess://%s@%s:%s/?alterID=%s", p.UUID, bracketHost(server), port, aid), nil
	}
}

func clashVless(p ClashProxy) (string, error) {
	if p.UUID == "" {
		return "", fmt.Errorf("vless missing uuid: %s", p.Name)
	}
	hostHeader, path := clashWSMeta(p)
	tlsOn := asBool(p.TLS)
	net := strings.ToLower(p.Network)
	server := p.Server
	port := anyToString(p.Port)
	// glider vless basic: vless://uuid@host:port
	// chain for transport
	switch {
	case net == "ws" && tlsOn:
		return fmt.Sprintf("wss://%s:%s%s?host=%s&skipVerify=%s,vless://%s@",
			server, port, path, hostHeader, skipVerifyStr(p), p.UUID), nil
	case net == "ws":
		return fmt.Sprintf("ws://%s:%s%s?host=%s,vless://%s@",
			server, port, path, hostHeader, p.UUID), nil
	case tlsOn:
		sni := firstNonEmpty(p.ServerName, p.SNI, hostHeader, server)
		return fmt.Sprintf("tls://%s:%s?serverName=%s&skipVerify=%s,vless://%s@",
			server, port, sni, skipVerifyStr(p), p.UUID), nil
	default:
		return fmt.Sprintf("vless://%s@%s", p.UUID, joinHostPort(server, p.Port)), nil
	}
}

func clashTrojan(p ClashProxy) (string, error) {
	pass := p.Password
	if pass == "" {
		return "", fmt.Errorf("trojan missing password: %s", p.Name)
	}
	sni := firstNonEmpty(p.SNI, p.ServerName, p.Server)
	hostport := joinHostPort(p.Server, p.Port)
	// password may contain '@' etc — build URL so userinfo is properly escaped
	// glider reads password from url.User.Username()
	u := &url.URL{
		Scheme: "trojan",
		User:   url.User(pass),
		Host:   hostport,
	}
	q := url.Values{}
	q.Set("serverName", sni)
	q.Set("skipVerify", skipVerifyStr(p))
	q.Set("cert", "")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func clashHTTP(p ClashProxy) (string, error) {
	hostport := joinHostPort(p.Server, p.Port)
	if p.Username != "" {
		return fmt.Sprintf("http://%s:%s@%s", p.Username, urlUserInfoEscape(p.Password), hostport), nil
	}
	return "http://" + hostport, nil
}

func clashSocks(p ClashProxy) (string, error) {
	hostport := joinHostPort(p.Server, p.Port)
	if p.Username != "" {
		return fmt.Sprintf("socks5://%s:%s@%s", p.Username, urlUserInfoEscape(p.Password), hostport), nil
	}
	return "socks5://" + hostport, nil
}

func clashWSMeta(p ClashProxy) (hostHeader, path string) {
	path = "/"
	hostHeader = p.Server

	if p.WSPath != "" {
		path = normalizePath(p.WSPath)
	}
	if p.WSHeaders != nil {
		if h, ok := p.WSHeaders["Host"]; ok && h != "" {
			hostHeader = h
		} else if h, ok := p.WSHeaders["host"]; ok && h != "" {
			hostHeader = h
		}
	}
	if p.WSOpts != nil {
		if v, ok := p.WSOpts["path"]; ok {
			path = normalizePath(fmt.Sprint(v))
		}
		if hmap, ok := p.WSOpts["headers"].(map[string]interface{}); ok {
			if h, ok := hmap["Host"]; ok && fmt.Sprint(h) != "" {
				hostHeader = fmt.Sprint(h)
			} else if h, ok := hmap["host"]; ok && fmt.Sprint(h) != "" {
				hostHeader = fmt.Sprint(h)
			}
		}
		// yaml may decode headers as map[string]string via intermediate
		if hmap, ok := p.WSOpts["headers"].(map[string]string); ok {
			if h := hmap["Host"]; h != "" {
				hostHeader = h
			} else if h := hmap["host"]; h != "" {
				hostHeader = h
			}
		}
	}
	return hostHeader, path
}

func joinHostPort(host string, port interface{}) string {
	p := anyToString(port)
	if p == "" {
		p = "0"
	}
	// net.JoinHostPort handles IPv6 brackets
	return net.JoinHostPort(host, p)
}

func bracketHost(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

func asBool(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		b, _ := strconv.ParseBool(t)
		return b
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	default:
		return false
	}
}

func skipVerifyStr(p ClashProxy) string {
	// default true for airport nodes (many use self-signed / CDN)
	if p.SkipCertVerify == nil {
		return "true"
	}
	if asBool(p.SkipCertVerify) {
		return "true"
	}
	return "false"
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// escape password for userinfo (minimal — keep common UUID-like tokens intact)
func urlUserInfoEscape(s string) string {
	// if already simple, return as-is (glider expects raw in many cases)
	if !strings.ContainsAny(s, "@:/?#[]") {
		return s
	}
	replacer := strings.NewReplacer(
		"@", "%40",
		":", "%3A",
		"/", "%2F",
		"?", "%3F",
		"#", "%23",
	)
	return replacer.Replace(s)
}
