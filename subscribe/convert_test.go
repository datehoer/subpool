package subscribe

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestVmessWS(t *testing.T) {
	info := map[string]interface{}{
		"v": "2", "ps": "test", "add": "1.2.3.4", "port": "443",
		"id": "11111111-1111-1111-1111-111111111111", "aid": "0",
		"net": "ws", "type": "none", "host": "example.com", "path": "/ray", "tls": "tls",
	}
	b, _ := json.Marshal(info)
	line := "vmess://" + base64.StdEncoding.EncodeToString(b)
	out, err := FormatConvert(line)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "wss://1.2.3.4:443/ray?host=example.com") {
		t.Fatalf("unexpected: %s", out)
	}
	t.Log(out)
}

func TestSS(t *testing.T) {
	ss := "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:password@8.8.8.8:8388"))
	out, err := FormatConvert(ss)
	if err != nil {
		t.Fatal(err)
	}
	if out != "ss://aes-256-gcm:password@8.8.8.8:8388" {
		t.Fatalf("unexpected: %s", out)
	}
}
