package subscribe

import (
	"os"
	"strings"
	"testing"
)

const sampleClash = `
mixed-port: 7890
proxies:
  - {name: ss1, server: 1.2.3.4, port: 8388, type: ss, cipher: aes-256-gcm, password: secret, udp: true}
  - {name: vmess-ws, server: "cdn.example.com", port: 80, type: vmess, uuid: 11111111-1111-1111-1111-111111111111, alterId: 0, cipher: auto, tls: false, network: ws, ws-opts: {path: /ray, headers: {Host: "real.example.com"}}}
  - {name: vmess-wss, server: "cdn.example.com", port: 443, type: vmess, uuid: 11111111-1111-1111-1111-111111111111, alterId: 0, tls: true, skip-cert-verify: true, network: ws, ws-opts: {path: /ray, headers: {Host: "real.example.com"}}}
  - {name: trojan1, server: tj.example.com, port: 443, type: trojan, password: pass-word-1, sni: tj.example.com, skip-cert-verify: false}
  - {name: vless-ws, server: 9.9.9.9, port: 443, type: vless, uuid: 22222222-2222-2222-2222-222222222222, tls: true, servername: sni.example.com, network: ws, ws-opts: {path: "/ws", headers: {Host: sni.example.com}}}
  - {name: hy2, server: x.com, port: 443, type: hysteria2, password: x}
proxy-groups:
  - name: auto
    type: select
    proxies: [ss1]
`

func TestParseClashYAML(t *testing.T) {
	fwds, err := ParseClashYAML([]byte(sampleClash))
	if err != nil {
		t.Fatal(err)
	}
	if len(fwds) != 6 {
		t.Fatalf("want 6 forwards, got %d: %#v", len(fwds), fwds)
	}
	joined := strings.Join(fwds, "\n")
	checks := []string{
		"ss://aes-256-gcm:secret@1.2.3.4:8388",
		"ws://cdn.example.com:80/ray?host=real.example.com,vmess://",
		"wss://cdn.example.com:443/ray?host=real.example.com&skipVerify=true,vmess://",
		"trojan://pass-word-1@tj.example.com:443",
		"wss://9.9.9.9:443/ws?host=sni.example.com",
		"hy2://x@x.com:443",
	}
	for _, c := range checks {
		if !strings.Contains(joined, c) {
			t.Fatalf("missing %q in:\n%s", c, joined)
		}
	}

	// password containing '@' must be percent-encoded for glider
	fwds2, err := ParseClashYAML([]byte(`
proxies:
  - {name: t, type: trojan, server: h.com, port: 443, password: pro@secret, sni: h.com, skip-cert-verify: true}
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(fwds2) != 1 || !strings.Contains(fwds2[0], "pro%40secret@") {
		t.Fatalf("want escaped @ in password, got %v", fwds2)
	}
}

func TestLooksLikeClash(t *testing.T) {
	if !LooksLikeClash(sampleClash) {
		t.Fatal("expected clash")
	}
}

func TestParseAndConvertClash(t *testing.T) {
	fwds, err := ParseAndConvert(sampleClash)
	if err != nil {
		t.Fatal(err)
	}
	if len(fwds) < 4 {
		t.Fatalf("got %d", len(fwds))
	}
}

func TestParseRealClashFiles(t *testing.T) {
	for _, p := range []string{
		"/home/proxy/dy.5468936.xyz.yaml",
		"/home/proxy/中国国际机场.yaml",
		"/home/proxy/sub.v7.al.yaml",
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Log("skip", p, err)
			continue
		}
		fwds, err := ParseAndConvert(string(b))
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		t.Logf("%s -> %d nodes", p, len(fwds))
		if len(fwds) == 0 {
			t.Errorf("%s empty", p)
		}
	}
}
