package subscribe

import (
	"fmt"
	"net/url"
	"strings"
)

// convertSSR recovered from proxy.convertSSR.
// Input:  ssr://base64(host:port:protocol:method:obfs:base64pass/?params)
// Output: ssr://method:pass@host:port?protocol=...&protocol_param=...&obfs=...&obfs_param=...
//
// Format string from binary:
//
//	ssr://%s:%s@%s:%s?protocol=%s&protocol_param=%s&obfs=%s&obfs_param=%s
func convertSSR(line string) (string, error) {
	raw := strings.TrimPrefix(line, "ssr://")
	raw = strings.TrimPrefix(raw, "SSR://")

	decoded, err := tryBase64Decode(raw)
	if err != nil {
		return "", fmt.Errorf("invalid format: %w", err)
	}
	decoded = strings.TrimSpace(decoded)

	// host:port:protocol:method:obfs:pass_b64/?query
	mainPart, queryPart, _ := strings.Cut(decoded, "/?")
	if queryPart == "" {
		// some links use only ?
		mainPart, queryPart, _ = strings.Cut(decoded, "?")
	}

	parts := strings.Split(mainPart, ":")
	if len(parts) < 6 {
		return "", fmt.Errorf("invalid body format")
	}

	host := parts[0]
	port := parts[1]
	protocol := parts[2]
	method := parts[3]
	obfs := parts[4]
	passB64 := strings.Join(parts[5:], ":") // password may contain ':'
	// password is base64
	pass := passB64
	if d, err := tryBase64Decode(strings.TrimSuffix(passB64, "/")); err == nil {
		pass = strings.TrimSpace(d)
	}
	if pass == "" {
		return "", fmt.Errorf("invalid password format")
	}

	var protoParam, obfsParam string
	if queryPart != "" {
		vals, _ := url.ParseQuery(queryPart)
		if v := vals.Get("protoparam"); v != "" {
			if d, err := tryBase64Decode(v); err == nil {
				protoParam = d
			} else {
				protoParam = v
			}
		}
		if v := vals.Get("obfsparam"); v != "" {
			if d, err := tryBase64Decode(v); err == nil {
				obfsParam = d
			} else {
				obfsParam = v
			}
		}
	}

	return fmt.Sprintf(
		"ssr://%s:%s@%s:%s?protocol=%s&protocol_param=%s&obfs=%s&obfs_param=%s",
		method, pass, host, port, protocol, url.QueryEscape(protoParam), obfs, url.QueryEscape(obfsParam),
	), nil
}
