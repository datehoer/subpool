// Package redact removes credentials and private URL details from diagnostics.
package redact

import (
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	quotedURLRE = regexp.MustCompile(`(?i)"[a-z][a-z0-9+.-]*://(?:[^"\\\r\n]|\\.)*"`)
	urlRE       = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"]+`)
	authInfoRE  = regexp.MustCompile(`(?im)(auth info:)[^\r\n]*`)
	headerRE    = regexp.MustCompile(`(?im)((?:proxy-)?authorization:)[^\r\n]*`)
)

// URL keeps only the origin of a structured URL. Other schemes may encode an
// entire node, including its password, in the authority, so hide them in full.
func URL(raw string) string {
	scheme, _, ok := strings.Cut(raw, "://")
	if !ok {
		return "[redacted]"
	}
	hidden := scheme + "://[redacted]"
	switch strings.ToLower(scheme) {
	case "http", "https", "mixed", "socks", "socks5", "socks5h":
	default:
		return hidden
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || strings.Contains(u.Host, ",") {
		return hidden
	}
	return u.Scheme + "://" + u.Host
}

// Text sanitizes URLs and the authentication diagnostics emitted by glider.
func Text(s string) string {
	// net/url errors quote malformed URLs, which may contain spaces or escapes.
	s = quotedURLRE.ReplaceAllStringFunc(s, func(quoted string) string {
		raw, err := strconv.Unquote(quoted)
		if err != nil {
			return `"[redacted]"`
		}
		return strconv.Quote(URL(raw))
	})
	s = urlRE.ReplaceAllStringFunc(s, URL)
	s = authInfoRE.ReplaceAllString(s, "${1} [redacted]")
	return headerRE.ReplaceAllString(s, "${1} [redacted]")
}

// Writer sanitizes complete log messages, as written by the standard logger.
type Writer struct {
	io.Writer
}

func (w Writer) Write(p []byte) (int, error) {
	s := Text(string(p))
	n, err := io.WriteString(w.Writer, s)
	if err != nil {
		return 0, err
	}
	if n != len(s) {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}
