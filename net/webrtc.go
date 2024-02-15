package net

import (
	"crypto/tls"
	"net"

	"github.com/unitoftime/rtcnet"
)

type WebRtcDialer struct {
	Url string
	TlsConfig *tls.Config
	Ordered bool
	IceServers []string
}
func (d WebRtcDialer) DialPipe() (Pipe, error) {
	_, host := parseSchemeHost(d.Url)
	conn, err := rtcnet.Dial(host, d.TlsConfig, d.Ordered, d.IceServers)
	return conn, err
}

func newWebRtcListener(c *ListenConfig) (*rtcListener, error) {
	listener, err := rtcnet.NewListener(c.host, rtcnet.ListenConfig{
		TlsConfig: c.TlsConfig,
		OriginPatterns: c.OriginPatterns,
		IceServers: c.IceServers,
	})
	if err != nil {
		return nil, err
	}

	sockListener := &rtcListener{
		listener: listener,
	}
	return sockListener, nil
}

type rtcListener struct {
	listener net.Listener
}

func (l *rtcListener) Accept() (Socket, error) {
	c, err := l.listener.Accept()
	if err != nil {
		return nil, err
	}

	return newAcceptedSocket(c), nil
}
func (l *rtcListener) Close() error {
	return l.listener.Close()
}
func (l *rtcListener) Addr() net.Addr {
	return l.listener.Addr()
}
