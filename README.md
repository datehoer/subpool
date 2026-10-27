# SubPool

[English](README.md) | [中文](README.zh-CN.md)

**Turn a proxy subscription into a local IP proxy pool**

Convert airport / proxy subscriptions (v2rayN base64 lists, Clash YAML) into a local HTTP / SOCKS5 proxy pool with load balancing, health checks, timed hot reload, and Hysteria2 support.

> Use this tool only in lawful, compliant scenarios. Follow local laws and the terms of your providers. Do **not** post real subscription URLs in issues or PRs.

## Features

- Parse common subscription formats: VMess / VLESS / SS / SSR / Trojan / Hysteria2 / Clash YAML
- Local listeners: `http://` / `socks5://` / `mixed://`
- Scheduling strategies: `rr` / `random` / `wrr` / `ha` / `lha` / `dh`
- Subscription refresh, node health checks, fail-over on dial errors
- Leveled logging (daily rotation), access log, simple stats HTTP

## Requirements

- Go 1.22+

## Install

```bash
git clone https://github.com/datehoer/subpool.git
cd subpool
go build -o subpool .
./subpool -version
./subpool -h
```

Or:

```bash
go install github.com/datehoer/subpool@latest
```

Prebuilt binaries (Linux / macOS / Windows) are available on the [Releases](https://github.com/datehoer/subpool/releases) page.

## Quick start

```bash
./subpool -l mixed://:6666 -u "https://example.com/subscribe?token=xxx"

# Log file + stats + access log
./subpool -l mixed://:6666 -u "URL" \
  -log ./logs/subpool.log -level info \
  -stats 127.0.0.1:9090 -access-log

# Config file
cp subpool.conf.example subpool.conf
# Edit subpool.conf (never commit real subscription URLs)
./subpool -config ./subpool.conf
```

Smoke test:

```bash
curl -sx "http://127.0.0.1:6666" https://api.ipify.org
curl -s http://127.0.0.1:9090/stats | head
curl -s http://127.0.0.1:9090/healthz
```

## Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-l` | Listen URL: `http://` / `socks5://` / `mixed://` | required |
| `-u` / `-f` | Subscription URL / file | at least one |
| `-s` | `rr` `random` `wrr` `ha` `lha` `dh` | `rr` |
| `-dr` | Dial fail-over retries | `3` |
| `-ri` | Subscription refresh interval (seconds), `0` = off | `3600` |
| `-c` `-ci` `-ct` | Health-check URL / interval / timeout | msft / 600 / 10 |
| **`-level`** | `debug` `info` `warn` `error` | `info` |
| **`-log`** | Log file or directory (**daily rotation**) | empty = stderr only |
| **`-log-nostd`** | When a log file is set, skip stderr | `false` |
| **`-access-log`** | Per-dial access log | `false` |
| **`-stats`** | Stats HTTP, e.g. `127.0.0.1:9090` | empty = off |
| **`-wi`** | Watchdog probe interval (seconds), `0` = off | `60` |
| **`-wf`** | Exit after this many failed probes in a row while nodes look alive, `0` = never | `0` |
| **`-version`** | Print version and exit | |
| `-v` | Legacy flag, same as `-level debug` | |
| `-config` | conflag config file | |

### Scheduling

| Strategy | Behavior |
|----------|----------|
| `rr` | Round-robin |
| `random` | Random |
| `wrr` | Latency-weighted round-robin |
| `ha` | Prefer a fixed live node |
| `lha` | Lowest latency |
| `dh` | Destination-host sticky |

On dial failure under any strategy, SubPool retries with another node up to `-dr` times.

### Log levels

- `debug`: node list, every health check, dial failure details
- `info`: startup, refresh, status changes, pool load
- `warn` / `error`: warnings and errors

Access log format (`-access-log`):

```text
INFO [access] tcp api.ipify.org:443 via node.example:443 ok 120ms tries=1
```

### Stats HTTP

- `GET /stats` or `/` — JSON: `pool` (node count, dial success/fail, uptime, …), `subscribe` (time, node count and error of the latest load), `watchdog` (the last day of probes) and `nodes` (address, protocol, health, latency; node URLs are left out because they hold credentials)
- `GET /healthz` — `ok` when at least one live node exists

### Watchdog

Every `-wi` seconds SubPool fetches the health-check URL (`-c`) through its own first listener, the path clients take. Probe results appear in `/stats`. With `-wf N`, N failed probes in a row while nodes still pass their health checks mean the process is stuck, so it exits and lets systemd restart it. An empty pool is an upstream outage that a restart can't fix, so it never triggers an exit. Under systemd with `WatchdogSec`, SubPool also pings systemd, and only while its pool lock is free, so a deadlock gets the service restarted too.

## Layout

```text
subpool/
├── main.go config.go usage.go stats.go
├── pool/              # hot-reload pool + scheduling
├── proxy/hy2/         # Hysteria2 dialer
├── subscribe/         # subscription parsers
├── pkg/xlog/          # leveled log + daily rotation
├── subpool.conf.example
├── systemd/subpool.service
├── README.md
└── README.zh-CN.md
```

## systemd

```bash
sudo mkdir -p /opt/subpool
sudo cp subpool /opt/subpool/
sudo cp subpool.conf.example /opt/subpool/subpool.conf
# Edit /opt/subpool/subpool.conf
sudo cp systemd/subpool.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now subpool
```

The unit uses `Type=notify` with `WatchdogSec=120` and `Restart=always`; add `wf=5` to the config to also restart on failed probes.

## Development

```bash
go test ./...
go build -o subpool .
```

## Dependencies & license

This project is released under **[GPL-3.0](LICENSE)**.

Main dependencies:

- [glider](https://github.com/nadoo/glider) v0.16.4 (GPL-3.0)
- [hysteria](https://github.com/apernet/hysteria) core/extras v2

Please also comply with the licenses of upstream projects.

## Security

- Keep real subscription URLs, node passwords, and credentials in local config only (e.g. `subpool.conf`), which is gitignored by default
- Config backups, `.env` files, `subscriptions/`, logs and local `.learnings/` notes are also ignored; keep reusable examples in `subpool.conf.example`
- Logs redact URL credentials, paths, query strings and fragments; log files use owner-only permissions. Stats still show subscription origins and node addresses
- If you expose the stats endpoint, add access control yourself
- Report vulnerabilities via GitHub Security Advisories or private contact; do not paste secrets in public issues
