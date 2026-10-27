package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nadoo/glider/rule"

	"github.com/datehoer/subpool/subscribe"
)

func TestSubscriptionStatsDoNotExposePrivateData(t *testing.T) {
	refreshMu.Lock()
	previous := lastRefresh
	refreshMu.Unlock()
	t.Cleanup(func() {
		refreshMu.Lock()
		lastRefresh = previous
		refreshMu.Unlock()
	})
	raw := "https://canary-user:canary-pass@example.com/private-path?token=canary-token"
	loadErr := fmt.Errorf("Get %q: timeout", raw)
	conf := &Config{SubURLs: []string{raw}, SubFiles: []string{"/private/operator/subscriptions/nodes.yaml"}}
	each := []subscribe.Loaded{
		{Err: loadErr},
		{Forwards: []string{"ss://method:node-pass@example.com:443"}},
	}
	noteRefresh(1, each, conf, loadErr)
	refreshMu.Lock()
	snapshot := lastRefresh
	refreshMu.Unlock()
	b, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"canary-user", "canary-pass", "private-path", "canary-token", "/private/operator", "node-pass"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("subscription stats contain private data: %s", secret)
		}
	}
	if len(snapshot.Sources) != 2 || snapshot.Sources[0].Name != "https://example.com" || snapshot.Sources[1].Name != "nodes.yaml" || snapshot.Sources[1].Nodes != 1 {
		t.Error("subscription diagnostics lost their source identities or node counts")
	}
}

func TestWatchdogStatsHideSecretsWithoutChangingProbe(t *testing.T) {
	raw := "https://canary-user:canary-pass@example.com/private-path?token=canary-token"
	conf := &Config{Strategy: rule.Strategy{Check: raw + "#expect=200"}}
	if got := probeTarget(conf); got != raw {
		t.Fatal("diagnostic redaction changed the actual probe URL")
	}
	w := &watchdog{}
	snapshot := w.snapshot(conf)
	if snapshot.Target != "https://example.com" {
		t.Fatal("watchdog stats should show only the target origin")
	}
}
