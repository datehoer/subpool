package hy2

import (
	"net"

	"github.com/apernet/hysteria/core/v2/client"
	"github.com/apernet/hysteria/extras/v2/obfs"
)

// salamanderConnFactory wraps UDP with hysteria salamander obfuscation.
type salamanderConnFactory struct {
	password string
}

func newSalamanderConnFactory(password string) client.ConnFactory {
	return &salamanderConnFactory{password: password}
}

func (f *salamanderConnFactory) New(addr net.Addr) (net.PacketConn, error) {
	base, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}
	obf, err := obfs.NewSalamanderObfuscator([]byte(f.password))
	if err != nil {
		_ = base.Close()
		return nil, err
	}
	return obfs.WrapPacketConn(base, obf), nil
}
