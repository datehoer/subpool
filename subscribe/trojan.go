package subscribe

import (
	"fmt"
	"net/url"
	"strings"
)

// convertTrojan recovered from proxy.convertTrojan.
// Format strings from binary:
//
//	trojan://%s@%s?serverName=%s&skipVerify=%s&cert=%s
//	trojan://%s@%s
//
// Skips grpc links: "目前暂不支持grpc的trojan链接%s"
// Skips passwords with special chars: "trojan密码中不能存在特殊字符，已忽略执行:%s"
func convertTrojan(line string) (string, error) {
	if strings.Contains(line, "type=grpc") {
		return "", fmt.Errorf("目前暂不支持grpc的trojan链接%s", line)
	}

	u, err := url.Parse(line)
	if err != nil {
		return "", err
	}

	pass, _ := u.User.Password()
	if pass == "" {
		// trojan://password@host:port  — user is password
		pass = u.User.Username()
	}
	if pass == "" {
		return "", fmt.Errorf("trojan password empty")
	}

	hostport := u.Host
	if hostport == "" {
		return "", fmt.Errorf("trojan host empty")
	}

	q := u.Query()
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}
	if sni == "" {
		// fallback hostname without port
		sni = u.Hostname()
	}

	skipVerify := "true"
	if v := q.Get("allowInsecure"); v == "0" || strings.EqualFold(v, "false") {
		skipVerify = "false"
	}
	cert := q.Get("cert")

	out := &url.URL{
		Scheme: "trojan",
		User:   url.User(pass), // escapes '@' etc for glider
		Host:   hostport,
	}
	if sni != "" || cert != "" || len(q) > 0 {
		qq := url.Values{}
		qq.Set("serverName", sni)
		qq.Set("skipVerify", skipVerify)
		qq.Set("cert", cert)
		out.RawQuery = qq.Encode()
	}
	return out.String(), nil
}
