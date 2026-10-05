package net

import (
	"context"
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
func (d WebRtcDialer) DialPipe(ctx context.Context) (Pipe, error) {
	_, host := parseSchemeHost(d.Url)
	conn, err := rtcnet.DialContext(ctx, host, d.TlsConfig, d.Ordered, d.IceServers)
	if err != nil {
		return nil, err
	}

	return pipeWrapper{conn, "webrtc"}, nil
}

// Dials the same listener over websockets, which it also serves for dialers that can't connect with webrtc. See FallbackDialer
func (d WebRtcDialer) WebsocketFallback() WebsocketDialer {
	_, host := parseSchemeHost(d.Url)
	return WebsocketDialer{
		Url: "wss://" + host + "/wss",
		TlsConfig: d.TlsConfig,
	}
}

func newWebRtcListener(c *ListenConfig) (*rtcListener, error) {
	listener, err := rtcnet.NewListener(c.host, rtcnet.ListenConfig{
		TlsConfig: c.TlsConfig,
		OriginPatterns: c.OriginPatterns,
		IceServers: c.IceServers,
		PublicIP: c.PublicIP,
		IceLite: c.IceLite,
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

	pipe := pipeWrapper{
		Conn: c,
		transport: "webrtc",
	}
	_, isRtc := c.(*rtcnet.Conn)
	if !isRtc {
		pipe.transport = "wss"
	}

	return newAcceptedSocket(pipe), nil
}
func (l *rtcListener) Close() error {
	return l.listener.Close()
}
func (l *rtcListener) Addr() net.Addr {
	return l.listener.Addr()
}
