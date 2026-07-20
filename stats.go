package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/datehoer/subpool/pkg/xlog"
	"github.com/datehoer/subpool/pool"
)

// startStatsServer serves JSON stats on GET / and GET /stats.
func startStatsServer(addr string, hot *pool.HotPool) {
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/stats" {
			http.NotFound(w, r)
			return
		}
		snap := hot.Snapshot()
		out := map[string]any{
			"app":     "subpool",
			"version": version,
			"time":    time.Now().Format(time.RFC3339),
			"pool":    snap,
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
