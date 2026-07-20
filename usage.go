package main

// SubPool — subscription → local proxy pool.

const banner = `
   ███████╗██╗   ██╗██████╗ ██████╗  ██████╗  ██████╗ ██╗     
   ██╔════╝██║   ██║██╔══██╗██╔══██╗██╔═══██╗██╔═══██╗██║     
   ███████╗██║   ██║██████╔╝██████╔╝██║   ██║██║   ██║██║     
   ╚════██║██║   ██║██╔══██╗██╔═══╝ ██║   ██║██║   ██║██║     
   ███████║╚██████╔╝██████╔╝██║     ╚██████╔╝╚██████╔╝███████╗
   ╚══════╝ ╚═════╝ ╚═════╝ ╚═╝      ╚═════╝  ╚═════╝ ╚══════╝
                 subscription → IP proxy pool     version: %s

                      订阅一键生成 IP 代理池
`

const usageText = `
SubPool — 订阅一键生成 IP 代理池

命令行用法: 

- 本地 mixed(http+socks5)
subpool -l mixed://:6666 -u "订阅链接"

- 公网带认证
subpool -l mixed://user:pass@:6666 -u "订阅链接" -f "订阅文件"

- 写日志 + 统计端口 + 访问日志
subpool -l mixed://:6666 -u "URL" -log ./logs/subpool.log -level info -stats 127.0.0.1:9090 -access-log

- 配置文件
subpool -config ./subpool.conf


协议: socks5 / http / ss / ssr / trojan / vmess / vless / hy2(hysteria2)

订阅: v2rayN base64 列表 · Clash YAML（自动识别）

调度 (-s): rr | random | wrr | ha | lha | dh
  rr      轮询（默认）
  random  随机
  wrr     加权轮询（延迟低权重大）
  ha      高可用（固定首选）
  lha     最低延迟
  dh      目的哈希粘滞

拨号失败自动换节点 (-dr，默认 3)

日志:
  -level debug|info|warn|error
  -log PATH          写文件（按天滚动）
  -log-nostd         只写文件不打 stderr
  -access-log        每次拨号访问日志
  -stats HOST:PORT   HTTP 统计 /stats /healthz

测试:
  curl -sx http://127.0.0.1:6666 https://api.ipify.org
  curl -s http://127.0.0.1:9090/stats

参数：
`

const usageExtra = `
示例配置文件见: subpool.conf.example
文档: README.md
`
