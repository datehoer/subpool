// Package hy2 registers a Hysteria2 dialer for glider.
//
// glider (latest included) has no built-in hysteria2. This package wraps
// github.com/apernet/hysteria/core/v2 as a glider proxy.Dialer.
//
// URL schemes:
//
//	hy2://password@host:port?sni=SNI&skipVerify=true
//	hysteria2://password@host:port?sni=SNI&insecure=1&obfs=salamander&obfsPassword=xxx
package hy2

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/apernet/hysteria/core/v2/client"
	"github.com/nadoo/glider/proxy"

	"github.com/datehoer/subpool/pkg/xlog"
)

func init() {
	proxy.RegisterDialer("hy2", NewDialer)
	proxy.RegisterDialer("hysteria2", NewDialer)
	proxy.AddUsage("hy2", `
Hysteria2 client scheme:
  hy2://password@host:port[?sni=SNI][&skipVerify=true][&insecure=1][&obfs=salamander][&obfsPassword=xxx]
  hysteria2://password@host:port[?sni=SNI][&skipVerify=true]
`)
}

// Hy2 is a Hysteria2 forward dialer.
type Hy2 struct {
	dialer proxy.Dialer

	addr       string // host:port
	auth       string
	serverName string
	insecure   bool
	obfsType   string
	obfsPass   string
	up, down   uint64 // bytes/sec, 0 = default BBR

	mu     sync.Mutex
	client client.Client
}

// NewDialer creates a hy2 dialer from URL.
func NewDialer(s string, d proxy.Dialer) (proxy.Dialer, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("[hy2] parse url: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("[hy2] host required")
	}

	host := u.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(u.Hostname(), "443")
	}

	pass := ""
	if u.User != nil {
		pass = u.User.Username()
		if p, ok := u.User.Password(); ok && p != "" {
			pass = u.User.Username() + ":" + p
		}
	}
	q := u.Query()
	if pass == "" {
		pass = first(q.Get("auth"), q.Get("password"))
	}
	if pass == "" {
		return nil, fmt.Errorf("[hy2] password required")
	}

	sni := first(q.Get("sni"), q.Get("serverName"), q.Get("peer"), u.Hostname())
	insecure := truthy(q.Get("skipVerify")) ||
		truthy(q.Get("insecure")) ||
		truthy(q.Get("skip-cert-verify"))

	obfs := q.Get("obfs")
	if obfs != "" && !strings.EqualFold(obfs, "none") && !strings.EqualFold(obfs, "salamander") {
		return nil, fmt.Errorf("[hy2] unsupported obfs: %s", obfs)
	}

	return &Hy2{
		dialer:     d,
		addr:       host,
		auth:       pass,
		serverName: sni,
		insecure:   insecure,
		obfsType:   obfs,
		obfsPass:   first(q.Get("obfsPassword"), q.Get("obfs-password"), q.Get("obfs_password")),
		up:         parseBandwidth(q.Get("up")),
		down:       parseBandwidth(q.Get("down")),
	}, nil
}

// Addr implements proxy.Dialer.
func (h *Hy2) Addr() string {
	if h.addr == "" && h.dialer != nil {
		return h.dialer.Addr()
	}
	return h.addr
}

// Dial opens a TCP stream to addr via Hysteria2.
func (h *Hy2) Dial(network, addr string) (net.Conn, error) {
	if network != "" && !strings.HasPrefix(network, "tcp") {
		return nil, fmt.Errorf("[hy2] network %s not supported", network)
	}
	c, err := h.getClient()
	if err != nil {
		return nil, err
	}
	conn, err := c.TCP(addr)
	if err != nil {
		h.invalidate()
		c, err2 := h.getClient()
		if err2 != nil {
			return nil, fmt.Errorf("[hy2] dial %s via %s: %w", addr, h.addr, err)
		}
		conn, err = c.TCP(addr)
		if err != nil {
			return nil, fmt.Errorf("[hy2] dial %s via %s: %w", addr, h.addr, err)
		}
	}
	return conn, nil
}

// DialUDP is not supported yet (session model differs from net.PacketConn).
func (h *Hy2) DialUDP(network, addr string) (net.PacketConn, error) {
	return nil, proxy.ErrNotSupported
}

func (h *Hy2) invalidate() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.client != nil {
		_ = h.client.Close()
		h.client = nil
	}
}

func (h *Hy2) getClient() (client.Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.client != nil {
		return h.client, nil
	}
	c, err := h.connectLocked()
	if err != nil {
		return nil, err
	}
	h.client = c
	return c, nil
}

func (h *Hy2) connectLocked() (client.Client, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", h.addr)
	if err != nil {
		return nil, fmt.Errorf("[hy2] resolve %s: %w", h.addr, err)
	}

	cfg := &client.Config{
		ServerAddr: udpAddr,
		Auth:       h.auth,
		TLSConfig: client.TLSConfig{
			ServerName:         h.serverName,
			InsecureSkipVerify: h.insecure,
		},
		BandwidthConfig: client.BandwidthConfig{
			MaxTx: h.up,
			MaxRx: h.down,
		},
		FastOpen: true,
	}

	if strings.EqualFold(h.obfsType, "salamander") {
		if h.obfsPass == "" {
			return nil, fmt.Errorf("[hy2] salamander obfs requires obfsPassword")
		}
		cfg.ConnFactory = newSalamanderConnFactory(h.obfsPass)
	}

	c, info, err := client.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("[hy2] connect %s: %w", h.addr, err)
	}
	xlog.Debugf("[hy2] connected %s sni=%s udp=%v", h.addr, h.serverName, info != nil && info.UDPEnabled)
	return c, nil
}

func first(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// parseBandwidth accepts "100 Mbps", "100Mbps", "100" (treated as Mbps if small), or raw bps-ish numbers.
// Returns bytes/sec as hysteria expects.
func parseBandwidth(s string) uint64 {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0
	}
	s = strings.ReplaceAll(s, " ", "")
	mult := float64(1)
	switch {
	case strings.HasSuffix(s, "gbps"):
		mult = 1e9 / 8
		s = strings.TrimSuffix(s, "gbps")
	case strings.HasSuffix(s, "mbps"):
		mult = 1e6 / 8
		s = strings.TrimSuffix(s, "mbps")
	case strings.HasSuffix(s, "kbps"):
		mult = 1e3 / 8
		s = strings.TrimSuffix(s, "kbps")
	case strings.HasSuffix(s, "g"):
		mult = 1e9 / 8
		s = strings.TrimSuffix(s, "g")
	case strings.HasSuffix(s, "m"):
		mult = 1e6 / 8
		s = strings.TrimSuffix(s, "m")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return uint64(f * mult)
}
