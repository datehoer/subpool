package subscribe

import (
	"fmt"
	"net"
	"strings"
)

// convertSS recovered from proxy.convertSS.
// Handles both:
//
//	ss://base64(method:pass)@host:port
//	ss://base64(method:pass@host:port)
//	ss://method:pass@host:port
//
// Output formats from binary:
//
//	ss://%s:%s@%s:%s
//	ss://%s%s@%s:%s   (method may already contain params)
func convertSS(line string) (string, error) {
	raw := line
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	}
	// strip fragment / remark  #name
	if i := strings.Index(raw, "#"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)

	// userinfo@host:port  form
	if at := strings.Index(raw, "@"); at >= 0 {
		userinfo := raw[:at]
		hostport := raw[at+1:]

		// userinfo may be base64(method:password)
		if decoded, err := tryBase64Decode(userinfo); err == nil && strings.Contains(decoded, ":") {
			userinfo = decoded
		}

		method, pass, ok := strings.Cut(userinfo, ":")
		if !ok {
			return "", fmt.Errorf("ss 链接格式不正确")
		}

		host, port, err := splitHostPortLoose(hostport)
		if err != nil {
			return "", fmt.Errorf("ss host和port解析错误: %s，忽略执行：%v", hostport, err)
		}
		return fmt.Sprintf("ss://%s:%s@%s:%s", method, pass, host, port), nil
	}

	// entire body base64
	decoded, err := tryBase64Decode(raw)
	if err != nil {
		return "", fmt.Errorf("ss 链接解码失败: %s", err)
	}
	// method:pass@host:port
	at := strings.Index(decoded, "@")
	if at < 0 {
		return "", fmt.Errorf("ss 链接格式不正确")
	}
	userinfo := decoded[:at]
	hostport := decoded[at+1:]
	method, pass, ok := strings.Cut(userinfo, ":")
	if !ok {
		return "", fmt.Errorf("ss 链接格式不正确")
	}
	host, port, err := splitHostPortLoose(hostport)
	if err != nil {
		return "", fmt.Errorf("ss host和port解析错误: %s，忽略执行：%v", hostport, err)
	}
	return fmt.Sprintf("ss://%s:%s@%s:%s", method, pass, host, port), nil
}

func splitHostPortLoose(hostport string) (host, port string, err error) {
	hostport = strings.TrimSpace(hostport)
	// strip query if any
	if i := strings.IndexAny(hostport, "/?"); i >= 0 {
		hostport = hostport[:i]
	}
	h, p, err := net.SplitHostPort(hostport)
	if err == nil {
		return h, p, nil
	}
	// fallback last colon (IPv4)
	if i := strings.LastIndex(hostport, ":"); i > 0 {
		return hostport[:i], hostport[i+1:], nil
	}
	return "", "", err
}
