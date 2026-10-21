package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/datehoer/subpool/pkg/xlog"
	"github.com/datehoer/subpool/pool"
)

// refreshState is the outcome of the latest subscription load, for /stats.
type refreshState struct {
	At    time.Time `json:"at"`
	Nodes int       `json:"nodes"`
	Error string    `json:"error,omitempty"`
}

var (
	refreshMu   sync.Mutex
	lastRefresh refreshState
)

func noteRefresh(nodes int, err error) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	lastRefresh = refreshState{At: time.Now(), Nodes: nodes}
	if err != nil {
		lastRefresh.Error = redact(err.Error())
	}
}

// startStatsServer serves JSON stats on GET / and GET /stats: pool counters, the latest
// subscription load, the watchdog's probes and every node (without its URL, which holds credentials).
func startStatsServer(addr string, hot *pool.HotPool, conf *Config) {
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/stats" {
			http.NotFound(w, r)
			return
		}
		refreshMu.Lock()
		refresh := lastRefresh
		refreshMu.Unlock()
		out := map[string]any{
			"app":       "subpool",
			"version":   version,
			"time":      time.Now().Format(time.RFC3339),
			"pool":      hot.Snapshot(),
			"subscribe": refresh,
			"watchdog":  wd.snapshot(conf),
			"nodes":     hot.Nodes(),
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	}
	mux.HandleFunc("/", handler)
	mux.HandleFunc("/stats", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, avail, _ := hot.Stats()
		if avail == 0 {
			http.Error(w, "no available nodes", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	xlog.Infof("[stats] listening on http://%s/stats", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			xlog.Errorf("[stats] server error: %v", err)
		}
	}()
}
