package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/datehoer/subpool/pkg/redact"
	"github.com/datehoer/subpool/pkg/xlog"
	"github.com/datehoer/subpool/pool"
	"github.com/datehoer/subpool/subscribe"
)

// refreshState is the outcome of the latest subscription load, for /stats.
type refreshState struct {
	At      time.Time     `json:"at"`
	Nodes   int           `json:"nodes"`
	Error   string        `json:"error,omitempty"`
	Sources []sourceState `json:"sources"`
}

// sourceState is one subscription at that load, in config order (u= lines, then f= lines). A URL
// shows only its origin: the rest carries the access token.
type sourceState struct {
	Kind      string `json:"kind"` // url or file
	Name      string `json:"name"`
	Nodes     int    `json:"nodes"`
	Available int    `json:"available"` // of those nodes, how many pass health checks now
	Ms        int64  `json:"ms"`
	Error     string `json:"error,omitempty"`
	forwards  []string
}

var (
	refreshMu   sync.Mutex
	lastRefresh refreshState
)

func noteRefresh(nodes int, each []subscribe.Loaded, conf *Config, err error) {
	sources := make([]sourceState, len(each))
	for i, l := range each {
		s := sourceState{Kind: "url", Nodes: len(l.Forwards), Ms: l.Took.Milliseconds(), forwards: l.Forwards}
		if i < len(conf.SubURLs) {
			s.Name = redact.URL(conf.SubURLs[i])
		} else {
			s.Kind, s.Name = "file", filepath.Base(conf.SubFiles[i-len(conf.SubURLs)])
		}
		if l.Err != nil {
			s.Error = redact.Text(l.Err.Error())
		}
		sources[i] = s
	}
	refreshMu.Lock()
	defer refreshMu.Unlock()
	lastRefresh = refreshState{At: time.Now(), Nodes: nodes, Sources: sources}
	if err != nil {
		lastRefresh.Error = redact.Text(err.Error())
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
		nodes := hot.Nodes()
		up := make(map[string]bool, len(nodes))
		for _, n := range nodes {
			up[n.URL] = n.Enabled
		}
		refresh.Sources = append([]sourceState(nil), refresh.Sources...) // counted per request, not stored
		for i := range refresh.Sources {
			for _, f := range refresh.Sources[i].forwards {
				if up[f] {
					refresh.Sources[i].Available++
				}
			}
		}
		out := map[string]any{
			"app":       "subpool",
			"version":   version,
			"time":      time.Now().Format(time.RFC3339),
			"pool":      hot.Snapshot(),
			"subscribe": refresh,
			"watchdog":  wd.snapshot(conf),
			"nodes":     nodes,
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
