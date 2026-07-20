package pool

import (
	"hash/fnv"
	"net"
	"testing"
	"time"
)

func TestLatencyWeight(t *testing.T) {
	if w := latencyWeight(0); w != 20 {
		t.Fatalf("unknown weight want 20 got %d", w)
	}
	if w := latencyWeight(int64(100 * time.Millisecond)); w != 50 {
		t.Fatalf("100ms want 50 got %d", w)
	}
	if w := latencyWeight(int64(50 * time.Millisecond)); w != 100 {
		t.Fatalf("50ms want 100 got %d", w)
	}
	if w := latencyWeight(int64(2500 * time.Millisecond)); w != 2 {
		t.Fatalf("2500ms want 2 got %d", w)
	}
}

func TestDHHostStickiness(t *testing.T) {
	idx := func(n int, dst string) int {
		host := dst
		if h, _, err := net.SplitHostPort(dst); err == nil {
			host = h
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(host))
		return int(h.Sum32()) % n
	}
	if idx(8, "example.com:443") != idx(8, "example.com:8443") {
		t.Fatal("same host should map to same bucket")
	}
}
