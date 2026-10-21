package pool

import (
	"errors"
	"net"
	"testing"

	"github.com/nadoo/glider/proxy"
	"github.com/nadoo/glider/rule"
)

type lifecycleDialer struct {
	addr   string
	closed int
}

func (d *lifecycleDialer) Addr() string { return d.addr }

func (d *lifecycleDialer) Dial(string, string) (net.Conn, error) {
	return nil, errors.New("not implemented")
}

func (d *lifecycleDialer) DialUDP(string, string) (net.PacketConn, error) {
	return nil, errors.New("not implemented")
}

func (d *lifecycleDialer) Close() error {
	d.closed++
	return nil
}

func TestSwapReusesUnchangedAndClosesRetiredForwarders(t *testing.T) {
	proxy.RegisterDialer("lifecycle-test", func(rawURL string, _ proxy.Dialer) (proxy.Dialer, error) {
		return &lifecycleDialer{addr: rawURL}, nil
	})

	p := New(rule.Strategy{Check: "disable"})
	if err := p.Swap([]string{"lifecycle-test://one"}); err != nil {
		t.Fatal(err)
	}
	first := p.fwdrs[0]
	firstDialer := first.Dialer.(*lifecycleDialer)

	if err := p.Swap([]string{"lifecycle-test://one"}); err != nil {
		t.Fatal(err)
	}
	if p.fwdrs[0] != first {
		t.Fatal("unchanged forwarder was recreated")
	}
	if firstDialer.closed != 0 {
		t.Fatalf("unchanged forwarder closed %d times", firstDialer.closed)
	}

	if err := p.Swap([]string{"lifecycle-test://two"}); err != nil {
		t.Fatal(err)
	}
	if firstDialer.closed != 1 {
		t.Fatalf("retired forwarder closed %d times, want 1", firstDialer.closed)
	}
	secondDialer := p.fwdrs[0].Dialer.(*lifecycleDialer)

	p.Close()
	if secondDialer.closed != 1 {
		t.Fatalf("active forwarder closed %d times on shutdown, want 1", secondDialer.closed)
	}
}

func TestSwapWithHealthChecksDisabledEnablesAllForwarders(t *testing.T) {
	proxy.RegisterDialer("lifecycle-test", func(rawURL string, _ proxy.Dialer) (proxy.Dialer, error) {
		return &lifecycleDialer{addr: rawURL}, nil
	})

	p := New(rule.Strategy{Check: "disable"})
	if err := p.Swap([]string{"lifecycle-test://one", "lifecycle-test://two"}); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if total, available, _ := p.Stats(); total != 2 || available != 2 {
		t.Fatalf("pool stats = %d total, %d available; want 2, 2", total, available)
	}
}
