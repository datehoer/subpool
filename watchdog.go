package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/datehoer/subpool/pkg/xlog"
	"github.com/datehoer/subpool/pool"
)

// The watchdog fetches the health-check URL through SubPool's own listener every -wi seconds, the
// path clients take: listener, auth, scheduling and a node. Results are served in /stats. When
// probes keep failing while nodes still pass their health checks, the process is wedged and exits
// (-wf) so that systemd restarts it. An empty pool is an upstream outage that a restart can't fix;
// it only shows in /stats and /healthz.

type probe struct {
	T   int64  `json:"t"`  // unix seconds
	Ms  int64  `json:"ms"` // -1 when the probe failed
	Err string `json:"err,omitempty"`
}

type watchdog struct {
	mu     sync.Mutex
	probes []probe // oldest first, one day at most
	fails  int     // failed probes in a row
}

var wd watchdog

type watchdogSnapshot struct {
	IntervalSec int     `json:"interval_sec"`
	FailLimit   int     `json:"fail_limit"`
	FailsInARow int     `json:"fails_in_a_row"`
	Target      string  `json:"target"`
	Probes      []probe `json:"probes"`
}

func (w *watchdog) snapshot(conf *Config) watchdogSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return watchdogSnapshot{
		IntervalSec: conf.WatchInterval,
		FailLimit:   conf.WatchFails,
		FailsInARow: w.fails,
		Target:      probeTarget(conf),
		Probes:      append([]probe{}, w.probes...),
	}
}

// probeTarget is the health-check URL without its #expect options.
func probeTarget(conf *Config) string {
	target := strings.SplitN(conf.Strategy.Check, "#", 2)[0]
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return "http://www.msftconnecttest.com/connecttest.txt"
	}
	return target
}

// probeClient goes through the first listener. Listeners other than http, socks5 and mixed are
// skipped by dialing through the pool directly.
func probeClient(listen string, hot *pool.HotPool) *http.Client {
	tr := &http.Transport{DisableKeepAlives: true}
	u, err := url.Parse(listen)
	if err == nil && (u.Scheme == "http" || u.Scheme == "mixed" || u.Scheme == "socks5") {
		host := u.Hostname()
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		scheme := "http"
		if u.Scheme == "socks5" {
			scheme = "socks5"
		}
		tr.Proxy = http.ProxyURL(&url.URL{Scheme: scheme, User: u.User, Host: net.JoinHostPort(host, u.Port())})
	} else {
		tr.DialContext = func(_ context.Context, network, addr string) (net.Conn, error) {
			c, _, err := hot.Dial(network, addr)
			return c, err
		}
	}
	return &http.Client{Transport: tr, Timeout: 15 * time.Second}
}

func startWatchdog(conf *Config, hot *pool.HotPool) {
	every := time.Duration(conf.WatchInterval) * time.Second
	keep := max(1, int(24*time.Hour/every))
	client := probeClient(conf.Listens[0], hot)
	target := probeTarget(conf)
	go func() {
		for range time.Tick(every) {
			p := probe{T: time.Now().Unix(), Ms: -1}
			start := time.Now()
			resp, err := client.Get(target)
			if err == nil {
				resp.Body.Close()
				// 407 is our own auth, 502 a failed dial; either way the path is broken
				if resp.StatusCode == http.StatusProxyAuthRequired || resp.StatusCode >= 500 {
					err = fmt.Errorf("HTTP %d", resp.StatusCode)
				}
			}
			if err == nil {
				p.Ms = time.Since(start).Milliseconds()
			} else {
				p.Err = redact(err.Error())
			}

			wd.mu.Lock()
			wd.probes = append(wd.probes, p)
			if len(wd.probes) > keep {
				wd.probes = wd.probes[len(wd.probes)-keep:]
			}
			if p.Ms < 0 {
				wd.fails++
			} else {
				wd.fails = 0
			}
			fails := wd.fails
			wd.mu.Unlock()

			if p.Ms >= 0 {
				continue
			}
			xlog.Warnf("[watchdog] probe failed (%d in a row): %s", fails, p.Err)
			if conf.WatchFails > 0 && fails >= conf.WatchFails {
				if _, avail, _ := hot.Stats(); avail > 0 {
					xlog.Errorf("[watchdog] %d probes failed while %d nodes pass health checks; exiting for a restart", fails, avail)
					xlog.Close()
					os.Exit(1)
				}
			}
		}
	}()
	xlog.Infof("[watchdog] probing %s every %ds, exit after %d failures (0 = never)", target, conf.WatchInterval, conf.WatchFails)
}

// notifySystemd reports READY=1 and, when the unit sets WatchdogSec, pings at half that period.
// Each ping takes the pool lock first, so a deadlocked pool stops the pings and systemd restarts it.
func notifySystemd(hot *pool.HotPool) {
	sdNotify("READY=1")
	usec, err := strconv.ParseInt(os.Getenv("WATCHDOG_USEC"), 10, 64)
	if err != nil || usec <= 0 {
		return
	}
	go func() {
		for range time.Tick(time.Duration(usec) * time.Microsecond / 2) {
			hot.Stats()
			sdNotify("WATCHDOG=1")
		}
	}()
}

// sdNotify sends one state line to systemd (Type=notify); outside systemd it does nothing.
func sdNotify(state string) {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return
	}
	conn, err := net.Dial("unixgram", sock)
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}

var urlRE = regexp.MustCompile(`(https?://)[^\s"]+`)

// redact hides URLs in error texts: subscription URLs carry their access token.
func redact(s string) string {
	return urlRE.ReplaceAllString(s, "${1}…")
}
