package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/nadoo/glider/proxy"

	_ "github.com/nadoo/glider/proxy/http"
	_ "github.com/nadoo/glider/proxy/mixed"
	_ "github.com/nadoo/glider/proxy/obfs"
	_ "github.com/nadoo/glider/proxy/socks5"
	_ "github.com/nadoo/glider/proxy/ss"
	_ "github.com/nadoo/glider/proxy/ssr"
	_ "github.com/nadoo/glider/proxy/tls"
	_ "github.com/nadoo/glider/proxy/trojan"
	_ "github.com/nadoo/glider/proxy/vless"
	_ "github.com/nadoo/glider/proxy/vmess"
	_ "github.com/nadoo/glider/proxy/ws"

	_ "github.com/datehoer/subpool/proxy/hy2"

	"github.com/datehoer/subpool/pkg/xlog"
	"github.com/datehoer/subpool/pool"
	"github.com/datehoer/subpool/subscribe"
)

var version = "0.0.1"

func main() {
	// early flags that should not require full config
	for _, a := range os.Args[1:] {
		if a == "-version" || a == "--version" {
			fmt.Printf("subpool %s\n", version)
			return
		}
	}

	color.New(color.FgHiCyan).Printf(banner+"\n", version)

	conf := parseConfig()
	defer xlog.Close()

	hot := pool.New(conf.Strategy)
	hot.SetDialRetries(conf.DialRetries)
	hot.SetAccessLog(conf.AccessLog)
	defer hot.Close()

	if err := reload(hot, conf, true); err != nil {
		xlog.Errorf("startup failed: %v", err)
		os.Exit(1)
	}

	for _, listen := range conf.Listens {
		local, err := proxy.ServerFromURL(listen, hot)
		if err != nil {
			xlog.Errorf("listen %q: %v", listen, err)
			os.Exit(1)
		}
		go local.ListenAndServe()
		xlog.Infof("[listen] %s", listen)
	}

	if conf.StatsAddr != "" {
		startStatsServer(conf.StatsAddr, hot, conf)
	}
	if conf.WatchInterval > 0 {
		startWatchdog(conf, hot)
	}
	notifySystemd(hot)

	if conf.RefreshInterval > 0 {
		go func() {
			t := time.NewTicker(time.Duration(conf.RefreshInterval) * time.Second)
			defer t.Stop()
			for range t.C {
				if err := reload(hot, conf, false); err != nil {
					xlog.Warnf("[subscribe] refresh failed: %v", err)
					continue
				}
				total, avail, gen := hot.Stats()
				xlog.Infof("[subscribe] refreshed ok gen=%d total=%d available=%d", gen, total, avail)
			}
		}()
		xlog.Infof("[subscribe] auto-refresh every %ds", conf.RefreshInterval)
	} else {
		xlog.Infof("[subscribe] auto-refresh disabled (-ri 0)")
	}

	xlog.Infof("[main] subpool %s ready level=%s access-log=%v", version, conf.LogLevel, conf.AccessLog)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	xlog.Infof("[main] shutting down (%v)", sig)
}

func reload(hot *pool.HotPool, conf *Config, fatalOnEmpty bool) (err error) {
	forwards, each, err := subscribe.LoadAll(conf.SubURLs, conf.SubFiles)
	defer func() { noteRefresh(len(forwards), each, conf, err) }()
	if err != nil {
		return err
	}
	if xlog.CurrentLevel() <= xlog.Debug {
		for _, f := range forwards {
			xlog.Debugf("[subscribe] node: %s", f)
		}
	}
	xlog.Infof("[subscribe] parsed %d forwarders", len(forwards))
	return hot.Swap(forwards)
}
