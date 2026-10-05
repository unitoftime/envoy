package net

import (
	"context"
	"crypto/tls"
	"io"
	"net"
)

// A webtransport pipe is one session carrying one bidirectional stream. The stream is reliable and ordered, and we frame it into messages.
// Unlike webrtc there is no signaling, ICE, DTLS or SCTP underneath, just a QUIC connection

type WebTransportDialer struct {
	Url string
	TlsConfig *tls.Config // Note: Unused in the browser, which only trusts certs from its own roots
}
func (d WebTransportDialer) DialPipe(ctx context.Context) (Pipe, error) {
	_, host := parseSchemeHost(d.Url)
	stream, err := dialWtStream(ctx, "https://" + host + "/", d.TlsConfig)
	if err != nil {
		return nil, err
	}
	pipe := newWtPipe(stream)

	// A stream is only announced to the listener once something is written to it, so we open it with an empty message. The listener consumes it before it accepts the pipe
	_, err = pipe.Write(nil)
	if err != nil {
		pipe.Close()
		return nil, err
	}

	return pipe, nil
}

// The session and its stream, which each platform provides
type wtStream interface {
	io.ReadWriter
	Close() error // Closes the whole session
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
}

type wtPipe struct {
	wtStream
	framer *framer
}
func newWtPipe(stream wtStream) *wtPipe {
	return &wtPipe{
		wtStream: stream,
		framer: newFramer(stream),
	}
}

func (p *wtPipe) Transport() string {
	return "webtransport"
}

func (p *wtPipe) Read(b []byte) (int, error) {
	return p.framer.Read(b)
}

func (p *wtPipe) Write(b []byte) (int, error) {
	return p.framer.Write(b)
}
