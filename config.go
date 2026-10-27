package main

import (
	"fmt"
	stdlog "log"
	"os"
	"strings"

	"github.com/nadoo/conflag"
	"github.com/nadoo/glider/pkg/log"
	"github.com/nadoo/glider/rule"

	"github.com/datehoer/subpool/pkg/redact"
	"github.com/datehoer/subpool/pkg/xlog"
)

// Config holds CLI options.
type Config struct {
	Verbose bool // legacy; mapped into log level if -level not "info"

	Listens         []string
	SubURLs         []string
	SubFiles        []string
	RefreshInterval int
	DialRetries     int

	// logging
	LogLevel  string // debug|info|warn|error
	LogFile   string // path or dir; empty = stderr only
	LogNoStd  bool   // if true with LogFile, do not also write stderr
	AccessLog bool   // per-dial access lines

	// ops
	StatsAddr     string // e.g. 127.0.0.1:9090 ; empty = disabled
	ShowVersion   bool
	WatchInterval int // seconds between watchdog probes; 0 = off
	WatchFails    int // failed probes in a row before exiting; 0 = never exit

	Strategy rule.Strategy
}

func parseConfig() *Config {
	// glider uses the standard logger, including for failed-auth credentials.
	stdlog.SetOutput(redact.Writer{Writer: os.Stderr})
	conf := &Config{}
	flag := conflag.New()
	flag.SetOutput(os.Stdout)

	conf.Strategy.Strategy = "rr"
	conf.Strategy.Check = "http://www.msftconnecttest.com/connecttest.txt#expect=200"
	conf.Strategy.CheckInterval = 600
	conf.Strategy.CheckTimeout = 10
	conf.Strategy.CheckLatencySamples = 10
	conf.Strategy.MaxFailures = 3
	conf.Strategy.DialTimeout = 3
	conf.RefreshInterval = 3600
	conf.DialRetries = 3
	conf.LogLevel = "info"
	conf.WatchInterval = 60

	flag.BoolVar(&conf.ShowVersion, "version", false, "打印版本并退出")
	flag.StringSliceVar(&conf.SubURLs, "u", nil, "订阅链接（支持 v2rayN base64 列表 / Clash YAML）")
	flag.StringSliceVar(&conf.SubFiles, "f", nil, "订阅文件（支持 v2rayN / Clash YAML）")
	flag.BoolVar(&conf.Verbose, "v", false, "等价于 -level debug（兼容旧参数）")
	flag.StringSliceUniqVar(&conf.Listens, "l", nil, "本地监听协议端口，支持 http、socks5、mixed")
	flag.StringVar(&conf.Strategy.Strategy, "s", "rr", `调度策略:
rr: 轮询
random: 随机
wrr: 加权轮询(延迟越低权重越高)
ha: 高可用(固定首选存活节点)
lha: 最低延迟
dh: 目的地址哈希(同目标粘滞)`)
	flag.StringVar(&conf.Strategy.Check, "c", conf.Strategy.Check, "节点健康检测 URL")
	flag.IntVar(&conf.Strategy.CheckInterval, "ci", 600, "节点健康检测间隔(秒)")
	flag.IntVar(&conf.Strategy.CheckTimeout, "ct", 10, "节点请求超时(秒)")
	flag.IntVar(&conf.RefreshInterval, "ri", 3600, "订阅自动刷新间隔(秒)，0 表示不自动刷新")
	flag.IntVar(&conf.DialRetries, "dr", 3, "拨号失败时换节点重试次数(fail-fast)")

	flag.StringVar(&conf.LogLevel, "level", "info", "日志级别: debug|info|warn|error")
	flag.StringVar(&conf.LogFile, "log", "", "日志文件路径或目录（按天滚动）；空则只打 stderr")
	flag.BoolVar(&conf.LogNoStd, "log-nostd", false, "写文件时不再同时输出到 stderr")
	flag.BoolVar(&conf.AccessLog, "access-log", false, "记录每次拨号访问日志(目标/节点/耗时)")
	flag.StringVar(&conf.StatsAddr, "stats", "", "统计 HTTP 监听地址，如 127.0.0.1:9090；空则关闭")
	flag.IntVar(&conf.WatchInterval, "wi", 60, "看门狗: 每隔几秒经本地监听抓一次健康检测 URL，结果见 /stats；0 表示关闭")
	flag.IntVar(&conf.WatchFails, "wf", 0, "看门狗: 有可用节点但连续几次抓取失败就退出，交给 systemd 重启；0 表示不退出")

	flag.Usage = func() {
		fmt.Fprint(flag.Output(), usageText)
		flag.PrintDefaults()
		fmt.Fprint(flag.Output(), usageExtra)
	}

	if err := flag.Parse(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", redact.Text(err.Error()))
		os.Exit(-1)
	}

	if conf.ShowVersion {
		fmt.Printf("subpool %s\n", version)
		os.Exit(0)
	}

	// -v implies debug when level left at default info
	if conf.Verbose && strings.EqualFold(conf.LogLevel, "info") {
		conf.LogLevel = "debug"
	}

	// alsoStderr: always when no file; with file unless -log-nostd
	alsoStd := conf.LogFile == "" || !conf.LogNoStd
	if err := xlog.Configure(conf.LogLevel, conf.LogFile, alsoStd); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: log init: %s\n", redact.Text(err.Error()))
		os.Exit(-1)
	}

	// glider internal logger: verbose when debug
	log.Set(xlog.CurrentLevel() <= xlog.Debug, 19)

	if len(conf.Listens) == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: 必须指定 -l 参数, 在本地开启监听端口.\n")
		os.Exit(-1)
	}
	if len(conf.SubURLs) == 0 && len(conf.SubFiles) == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: 必须指定 -u 或 -f 参数, 至少一个订阅链接或本地订阅文件.\n")
		os.Exit(-1)
	}

	return conf
}
