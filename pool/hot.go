package pool

import (
	"context"
	"errors"
	"hash/fnv"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nadoo/glider/proxy"
	"github.com/nadoo/glider/rule"

	"github.com/datehoer/subpool/pkg/xlog"
)

// HotPool is a reloadable proxy.Proxy with stoppable health checks.
type HotPool struct {
	strategy    rule.Strategy
	dialRetries int
	accessLog   bool

	swapMu  sync.Mutex
	mu      sync.RWMutex
	fwdrs   []*rule.Forwarder
	avail   []*rule.Forwarder
	index   uint32
	wrrMu   sync.Mutex
	wrrCurr map[*rule.Forwarder]int
	cancel  context.CancelFunc
	gen     uint64
	rng     *rand.Rand
	rngMu   sync.Mutex
	started time.Time

	// stats
	dialOK    atomic.Uint64
	dialFail  atomic.Uint64
	dialRetry atomic.Uint64
}

// StatsSnapshot is exported pool stats for /stats.
type StatsSnapshot struct {
	Generation  uint64 `json:"generation"`
	Total       int    `json:"total_nodes"`
	Available   int    `json:"available_nodes"`
	Strategy    string `json:"strategy"`
	DialRetries int    `json:"dial_retries"`
	DialOK      uint64 `json:"dial_ok"`
	DialFail    uint64 `json:"dial_fail"`
	DialRetry   uint64 `json:"dial_retry"`
	UptimeSec   int64  `json:"uptime_sec"`
	AccessLog   bool   `json:"access_log"`
	CheckEvery  int    `json:"check_interval_sec"`
}

// NodeStat is one node in /stats. The node URL is left out: it holds the node's credentials.
type NodeStat struct {
	URL       string `json:"-"`
	Addr      string `json:"addr"`
	Kind      string `json:"kind"` // protocols of the chain, e.g. "trojan" or "simple-obfs+ss"
	Enabled   bool   `json:"enabled"`
	LatencyMs int64  `json:"latency_ms"`
	Failures  uint32 `json:"failures"`
}

// New creates an empty HotPool.
func New(strategy rule.Strategy) *HotPool {
	if strategy.CheckInterval <= 0 {
		strategy.CheckInterval = 600
	}
	if strategy.CheckTimeout <= 0 {
		strategy.CheckTimeout = 10
	}
	if strategy.MaxFailures <= 0 {
		strategy.MaxFailures = 3
	}
	if strategy.DialTimeout <= 0 {
		strategy.DialTimeout = 3
	}
	if strategy.Strategy == "" {
		strategy.Strategy = "rr"
	}
	if strategy.Check == "" {
		strategy.Check = "http://www.msftconnecttest.com/connecttest.txt#expect=200"
	}
	return &HotPool{
		strategy:    strategy,
		dialRetries: 3,
		wrrCurr:     make(map[*rule.Forwarder]int),
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
		started:     time.Now(),
	}
}

// SetDialRetries sets fail-fast retries.
func (p *HotPool) SetDialRetries(n int) {
	if n <= 0 {
		n = 1
	}
	p.dialRetries = n
}

// SetAccessLog enables per-dial access logging.
func (p *HotPool) SetAccessLog(on bool) {
	p.accessLog = on
}

// Swap replaces forwarders and restarts health checks.
func (p *HotPool) Swap(forwards []string) error {
	if len(forwards) == 0 {
		return errors.New("no forwarders")
	}
	p.swapMu.Lock()
	defer p.swapMu.Unlock()

	p.mu.RLock()
	reusable := make(map[string][]*rule.Forwarder, len(p.fwdrs))
	for _, f := range p.fwdrs {
		reusable[f.URL()] = append(reusable[f.URL()], f)
	}
	p.mu.RUnlock()

	dialTO := time.Duration(p.strategy.DialTimeout) * time.Second
	relayTO := time.Duration(p.strategy.RelayTimeout) * time.Second

	fwdrs := make([]*rule.Forwarder, 0, len(forwards))
	for _, chain := range forwards {
		if existing := reusable[chain]; len(existing) > 0 {
			f := existing[len(existing)-1]
			reusable[chain] = existing[:len(existing)-1]
			f.SetMaxFailures(uint32(p.strategy.MaxFailures))
			fwdrs = append(fwdrs, f)
			continue
		}
		f, err := rule.ForwarderFromURL(chain, p.strategy.IntFace, dialTO, relayTO)
		if err != nil {
			xlog.Warnf("[pool] skip invalid forwarder %s: %v", chain, err)
			continue
		}
		f.SetMaxFailures(uint32(p.strategy.MaxFailures))
		f.AddHandler(func(fw *rule.Forwarder) {
			p.onStatusChanged(fw)
		})
		fwdrs = append(fwdrs, f)
	}
	if len(fwdrs) == 0 {
		return errors.New("no valid forwarders after parse")
	}

	var stale []*rule.Forwarder
	for _, old := range reusable {
		stale = append(stale, old...)
	}

	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.fwdrs = fwdrs
	p.avail = nil
	p.index = 0
	p.gen++
	gen := p.gen
	p.wrrMu.Lock()
	p.wrrCurr = make(map[*rule.Forwarder]int)
	p.wrrMu.Unlock()
	p.rebuildAvail()
	p.mu.Unlock()

	if len(fwdrs) == 1 {
		fwdrs[0].Enable()
		closeForwarders(stale)
		xlog.Infof("[pool] gen=%d loaded 1 node strategy=%s", gen, p.strategy.Strategy)
		return nil
	}

	closeForwarders(stale)

	xlog.Infof("[pool] gen=%d loaded %d nodes, strategy=%s, dial-retries=%d, check every %ds",
		gen, len(fwdrs), p.strategy.Strategy, p.dialRetries, p.strategy.CheckInterval)

	checker, err := newChecker(p.strategy)
	if err != nil {
		xlog.Warnf("[pool] health check disabled: %v", err)
		for _, f := range fwdrs {
			f.Enable()
		}
		p.mu.Lock()
		p.rebuildAvail()
		p.mu.Unlock()
		return nil
	}

	for _, f := range fwdrs {
		go p.checkLoop(ctx, f, checker)
	}
	return nil
}

// Close stops health checks.
func (p *HotPool) Close() {
	p.swapMu.Lock()
	defer p.swapMu.Unlock()
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	fwdrs := p.fwdrs
	p.fwdrs = nil
	p.avail = nil
	p.mu.Unlock()
	closeForwarders(fwdrs)
}

type dialerCloser interface {
	Close() error
}

func closeForwarders(forwarders []*rule.Forwarder) {
	for _, f := range forwarders {
		closer, ok := f.Dialer.(dialerCloser)
		if !ok {
			continue
		}
		if err := closer.Close(); err != nil {
			xlog.Warnf("[pool] close retired forwarder failed: %v", err)
		}
	}
}

func (p *HotPool) onStatusChanged(fw *rule.Forwarder) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rebuildAvail()
	if fw.Enabled() {
		xlog.Infof("[状态] %s 开启 (%d/%d)", fw.Addr(), len(p.avail), len(p.fwdrs))
	} else {
		xlog.Warnf("[状态] %s 关闭 (%d/%d)", fw.Addr(), len(p.avail), len(p.fwdrs))
	}
}

func (p *HotPool) rebuildAvail() {
	avail := make([]*rule.Forwarder, 0, len(p.fwdrs))
	for _, f := range p.fwdrs {
		if f.Enabled() {
			avail = append(avail, f)
		}
	}
	p.avail = avail
}

// --- proxy.Proxy ---

// glider's HTTP and SOCKS5 servers log dialer.Addr() when a dial fails, so a failed Dial returns
// the last forwarder it tried, or this empty one; a nil dialer there crashes the process.
var noForwarder = &rule.Forwarder{}

func (p *HotPool) Dial(network, addr string) (net.Conn, proxy.Dialer, error) {
	retries := p.dialRetries
	if retries <= 0 {
		retries = 1
	}
	tried := make(map[*rule.Forwarder]struct{}, retries)
	var lastErr error
	last := noForwarder
	start := time.Now()

	for i := 0; i < retries; i++ {
		d := p.nextExcluding(addr, tried)
		if d == nil {
			break
		}
		tried[d] = struct{}{}
		last = d

		c, err := d.Dial(network, addr)
		if err == nil {
			p.dialOK.Add(1)
			if i > 0 {
				p.dialRetry.Add(1)
				xlog.Debugf("[pool] dial %s ok via %s after %d fail(s)", addr, d.Addr(), i)
			}
			if p.accessLog {
				xlog.Accessf("tcp %s via %s ok %dms tries=%d", addr, d.Addr(), time.Since(start).Milliseconds(), i+1)
			}
			return c, d, nil
		}
		lastErr = err
		p.dialFail.Add(1)
		xlog.Debugf("[pool] dial %s via %s failed (%d/%d): %v", addr, d.Addr(), i+1, retries, err)
	}

	if lastErr == nil {
		lastErr = errors.New("no available forwarder")
	}
	if p.accessLog {
		xlog.Accessf("tcp %s FAIL %dms tries=%d err=%v", addr, time.Since(start).Milliseconds(), len(tried), lastErr)
	}
	return nil, last, lastErr
}

func (p *HotPool) DialUDP(network, addr string) (net.PacketConn, proxy.UDPDialer, error) {
	retries := p.dialRetries
	if retries <= 0 {
		retries = 1
	}
	tried := make(map[*rule.Forwarder]struct{}, retries)
	var lastErr error
	last := noForwarder
	start := time.Now()

	for i := 0; i < retries; i++ {
		d := p.nextExcluding(addr, tried)
		if d == nil {
			break
		}
		tried[d] = struct{}{}
		last = d
		pc, err := d.DialUDP(network, addr)
		if err == nil {
			p.dialOK.Add(1)
			if p.accessLog {
				xlog.Accessf("udp %s via %s ok %dms", addr, d.Addr(), time.Since(start).Milliseconds())
			}
			return pc, d, nil
		}
		lastErr = err
		p.dialFail.Add(1)
	}
	if lastErr == nil {
		lastErr = errors.New("no available forwarder")
	}
	return nil, last, lastErr
}

func (p *HotPool) NextDialer(dstAddr string) proxy.Dialer {
	if d := p.nextExcluding(dstAddr, nil); d != nil {
		return d
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.fwdrs) > 0 {
		return p.fwdrs[0]
	}
	return nil
}

func (p *HotPool) Record(dialer proxy.Dialer, success bool) {
	if fw, ok := dialer.(*rule.Forwarder); ok {
		if !success {
			fw.IncFailures()
			return
		}
		fw.Enable()
	}
}

func (p *HotPool) nextExcluding(dstAddr string, tried map[*rule.Forwarder]struct{}) *rule.Forwarder {
	p.mu.RLock()
	list := p.avail
	if len(list) == 0 {
		list = p.fwdrs
	}
	cands := make([]*rule.Forwarder, 0, len(list))
	for _, f := range list {
		if tried != nil {
			if _, ok := tried[f]; ok {
				continue
			}
		}
		cands = append(cands, f)
	}
	p.mu.RUnlock()

	if len(cands) == 0 {
		return nil
	}
	if len(cands) == 1 {
		return cands[0]
	}

	switch strings.ToLower(p.strategy.Strategy) {
	case "ha":
		return cands[0]
	case "lha":
		return pickLHA(cands)
	case "random", "rand":
		return p.pickRandom(cands)
	case "wrr":
		return p.pickWRR(cands)
	case "dh":
		return pickDH(cands, dstAddr)
	default:
		i := atomic.AddUint32(&p.index, 1)
		return cands[int(i)%len(cands)]
	}
}

func pickLHA(list []*rule.Forwarder) *rule.Forwarder {
	best := list[0]
	bestLat := best.Latency()
	for _, f := range list[1:] {
		lat := f.Latency()
		if lat > 0 && (bestLat == 0 || lat < bestLat) {
			best = f
			bestLat = lat
		}
	}
	return best
}

func (p *HotPool) pickRandom(list []*rule.Forwarder) *rule.Forwarder {
	p.rngMu.Lock()
	i := p.rng.Intn(len(list))
	p.rngMu.Unlock()
	return list[i]
}

func (p *HotPool) pickWRR(list []*rule.Forwarder) *rule.Forwarder {
	p.wrrMu.Lock()
	defer p.wrrMu.Unlock()

	total := 0
	var best *rule.Forwarder
	bestCurr := -1 << 30
	for _, f := range list {
		w := latencyWeight(f.Latency())
		p.wrrCurr[f] += w
		total += w
		if p.wrrCurr[f] > bestCurr {
			bestCurr = p.wrrCurr[f]
			best = f
		}
	}
	if best == nil {
		return list[0]
	}
	p.wrrCurr[best] -= total
	return best
}

func latencyWeight(lat int64) int {
	if lat <= 0 {
		return 20
	}
	ms := lat / int64(time.Millisecond)
	if ms <= 0 {
		ms = 1
	}
	w := int(5000 / ms)
	if w < 2 {
		w = 2
	}
	if w > 100 {
		w = 100
	}
	return w
}

func pickDH(list []*rule.Forwarder, dstAddr string) *rule.Forwarder {
	host := dstAddr
	if h, _, err := net.SplitHostPort(dstAddr); err == nil {
		host = h
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(host))
	return list[int(h.Sum32())%len(list)]
}

// Stats returns total/available/gen (compat).
func (p *HotPool) Stats() (total, available int, gen uint64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.fwdrs), len(p.avail), p.gen
}

// Snapshot returns full stats for HTTP /stats.
func (p *HotPool) Snapshot() StatsSnapshot {
	total, avail, gen := p.Stats()
	return StatsSnapshot{
		Generation:  gen,
		Total:       total,
		Available:   avail,
		Strategy:    p.strategy.Strategy,
		DialRetries: p.dialRetries,
		DialOK:      p.dialOK.Load(),
		DialFail:    p.dialFail.Load(),
		DialRetry:   p.dialRetry.Load(),
		UptimeSec:   int64(time.Since(p.started).Seconds()),
		AccessLog:   p.accessLog,
		CheckEvery:  p.strategy.CheckInterval,
	}
}

// Nodes returns every node with its health-check state.
func (p *HotPool) Nodes() []NodeStat {
	p.mu.RLock()
	fwdrs := p.fwdrs // Swap installs a new slice, so this one stays intact
	p.mu.RUnlock()
	nodes := make([]NodeStat, 0, len(fwdrs))
	for _, f := range fwdrs {
		var kinds []string
		for _, part := range strings.Split(strings.SplitN(f.URL(), "#", 2)[0], ",") {
			if i := strings.Index(part, "://"); i > 0 {
				kinds = append(kinds, part[:i])
			}
		}
		nodes = append(nodes, NodeStat{
			URL:       f.URL(),
			Addr:      f.Addr(),
			Kind:      strings.Join(kinds, "+"),
			Enabled:   f.Enabled(),
			LatencyMs: f.Latency() / int64(time.Millisecond),
			Failures:  f.Failures(),
		})
	}
	return nodes
}

// --- health check ---

type checker interface {
	Check(fw *rule.Forwarder) (time.Duration, error)
}

func newChecker(s rule.Strategy) (checker, error) {
	check := s.Check
	if check == "" || check == "disable" {
		return nil, errors.New("disabled")
	}
	if !strings.Contains(check, "://") {
		check += "://"
	}
	u, err := url.Parse(check)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(s.CheckTimeout) * time.Second
	switch u.Scheme {
	case "http", "https":
		expect := "200"
		if params, _ := url.ParseQuery(u.Fragment); params.Get("expect") != "" {
			expect = params.Get("expect")
		}
		return &httpChecker{
			url:     u.Scheme + "://" + u.Host + u.RequestURI(),
			expect:  expect,
			timeout: timeout,
		}, nil
	case "tcp":
		addr := u.Host
		if addr == "" {
			addr = "1.1.1.1:443"
		}
		return &tcpChecker{addr: addr, timeout: timeout}, nil
	default:
		return nil, errors.New("unknown check scheme: " + u.Scheme)
	}
}

type httpChecker struct {
	url     string
	expect  string
	timeout time.Duration
}

func (c *httpChecker) Check(fw *rule.Forwarder) (time.Duration, error) {
	start := time.Now()
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return fw.Dial(network, addr)
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: transport, Timeout: c.timeout}
	resp, err := client.Get(c.url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	code := strconvItoa(resp.StatusCode)
	if c.expect != "" && !strings.Contains(code, c.expect) && !strings.Contains(resp.Status, c.expect) {
		if c.expect != code {
			return 0, errors.New("unexpected status: " + resp.Status)
		}
	}
	return time.Since(start), nil
}

type tcpChecker struct {
	addr    string
	timeout time.Duration
}

func (c *tcpChecker) Check(fw *rule.Forwarder) (time.Duration, error) {
	start := time.Now()
	conn, err := fw.Dial("tcp", c.addr)
	if err != nil {
		return 0, err
	}
	conn.Close()
	return time.Since(start), nil
}

func (p *HotPool) checkLoop(ctx context.Context, fw *rule.Forwarder, checker checker) {
	interval := time.Duration(p.strategy.CheckInterval) * time.Second
	timer := time.NewTimer(0)
	defer timer.Stop()
	wait := 1
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if p.strategy.CheckDisabledOnly && fw.Enabled() {
			timer.Reset(interval)
			continue
		}
		elapsed, err := checker.Check(fw)
		if err != nil {
			xlog.Debugf("[节点检测] %s 失败: %v", fw.Addr(), err)
			fw.Disable()
			wait++
			if wait > 16 {
				wait = 16
			}
			timer.Reset(interval * time.Duration(wait))
			continue
		}
		wait = 1
		samples := p.strategy.CheckLatencySamples
		if samples <= 1 {
			fw.SetLatency(int64(elapsed))
		} else {
			last := fw.Latency()
			if last <= 0 {
				fw.SetLatency(int64(elapsed))
			} else {
				fw.SetLatency((last*(int64(samples)-1) + int64(elapsed)) / int64(samples))
			}
		}
		xlog.Debugf("[节点检测] %s 成功 延迟=%dms", fw.Addr(), elapsed.Milliseconds())
		fw.Enable()
		timer.Reset(interval)
	}
}

func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
