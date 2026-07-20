# SubPool

**订阅一键生成 IP 代理池**

把机场 / 代理订阅（v2rayN、Clash YAML）转成本地 HTTP / SOCKS5 代理池，支持轮询、健康检查、定时热更新与 Hysteria2。

> 请仅在合法、合规的前提下使用本工具，并遵守当地法律法规与服务条款。不要把真实订阅链接提交到 issue 或 PR。

## 功能

- 解析常见订阅：VMess / VLESS / SS / SSR / Trojan / Hysteria2 / Clash YAML
- 本地监听：`http://` / `socks5://` / `mixed://`
- 调度策略：`rr` / `random` / `wrr` / `ha` / `lha` / `dh`
- 订阅定时刷新、节点健康检查、拨号失败换节点
- 分级日志（按天滚动）、访问日志、简易 stats HTTP

## 要求

- Go 1.22+

## 安装

```bash
git clone https://github.com/datehoer/subpool.git
cd subpool
go build -o subpool .
./subpool -version
./subpool -h
```

或直接：

```bash
go install github.com/datehoer/subpool@latest
```

## 快速开始

```bash
./subpool -l mixed://:6666 -u "https://example.com/subscribe?token=xxx"

# 日志文件 + 统计 + 访问日志
./subpool -l mixed://:6666 -u "URL" \
  -log ./logs/subpool.log -level info \
  -stats 127.0.0.1:9090 -access-log

# 配置文件
cp subpool.conf.example subpool.conf
# 编辑 subpool.conf（勿提交含真实订阅的配置）
./subpool -config ./subpool.conf
```

测试：

```bash
curl -sx "http://127.0.0.1:6666" https://api.ipify.org
curl -s http://127.0.0.1:9090/stats | head
curl -s http://127.0.0.1:9090/healthz
```

## 参数一览

| 参数 | 含义 | 默认 |
|------|------|------|
| `-l` | 监听 `http://` / `socks5://` / `mixed://` | 必填 |
| `-u` / `-f` | 订阅 URL / 文件 | 至少一个 |
| `-s` | `rr` `random` `wrr` `ha` `lha` `dh` | `rr` |
| `-dr` | 拨号失败换节点次数 | `3` |
| `-ri` | 订阅刷新秒，`0` 关 | `3600` |
| `-c` `-ci` `-ct` | 健康检查 URL / 间隔 / 超时 | msft / 600 / 10 |
| **`-level`** | `debug` `info` `warn` `error` | `info` |
| **`-log`** | 日志文件或目录（**按天滚动**） | 空=仅 stderr |
| **`-log-nostd`** | 有文件时不再打 stderr | `false` |
| **`-access-log`** | 每次拨号访问日志 | `false` |
| **`-stats`** | 统计 HTTP，如 `127.0.0.1:9090` | 空=关 |
| **`-version`** | 打印版本退出 | |
| `-v` | 兼容旧参数，等价 `-level debug` | |
| `-config` | conflag 配置文件 | |

### 调度

| 策略 | 行为 |
|------|------|
| `rr` | 轮询 |
| `random` | 随机 |
| `wrr` | 延迟加权轮询 |
| `ha` | 固定首选存活节点 |
| `lha` | 最低延迟 |
| `dh` | 同目标主机粘滞 |

任意策略下 Dial 失败会按 `-dr` 换节点重试。

### 日志级别

- `debug`：节点列表、每次健康检查、拨号失败细节
- `info`：启动、刷新、状态变更、池加载
- `warn` / `error`：告警与错误

访问日志格式（`-access-log`）：

```text
INFO [access] tcp api.ipify.org:443 via node.example:443 ok 120ms tries=1
```

### 统计 HTTP

- `GET /stats` 或 `/` — JSON（节点数、拨号成功/失败、uptime…）
- `GET /healthz` — 有存活节点则 `ok`

## 目录

```text
subpool/
├── main.go config.go usage.go stats.go
├── pool/              # 热更新节点池 + 调度
├── proxy/hy2/         # Hysteria2 dialer
├── subscribe/         # 订阅解析
├── pkg/xlog/          # 分级日志 + 文件滚动
├── subpool.conf.example
├── systemd/subpool.service
└── README.md
```

## systemd

```bash
sudo mkdir -p /opt/subpool
sudo cp subpool /opt/subpool/
sudo cp subpool.conf.example /opt/subpool/subpool.conf
# 编辑 /opt/subpool/subpool.conf
sudo cp systemd/subpool.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now subpool
```

## 开发

```bash
go test ./...
go build -o subpool .
```

## 依赖与许可证

本项目以 **[GPL-3.0](LICENSE)** 发布。

主要依赖：

- [glider](https://github.com/nadoo/glider) v0.16.4（GPL-3.0）
- [hysteria](https://github.com/apernet/hysteria) core/extras v2

请同时遵守各上游项目的许可证条款。

## 安全说明

- 真实订阅 URL、节点密码、认证信息只应放在本地配置（如 `subpool.conf`），且已被 `.gitignore` 忽略
- 若 `stats` 暴露在公网，请自行做好访问控制
- 漏洞报告请通过 GitHub Security Advisories 或私信维护者，勿在公开 issue 中贴敏感信息
