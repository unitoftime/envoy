//go:build !js
// +build !js

package net

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

const (
	wtOpenTimeout = 10 * time.Second // How long an accepted session has to open its stream
	wtKeepAlive = 10 * time.Second // Keeps a quiet connection from idling out
)

func wtQuicConfig() *quic.Config {
	return &quic.Config{
		KeepAlivePeriod: wtKeepAlive,

		// Note: We don't use either, but webtransport requires both
		EnableDatagrams: true,
		EnableStreamResetPartialDelivery: true,
	}
}

type quicStream struct {
	*webtransport.Stream
	session *webtransport.Session
}

// Note: Closing the session also closes its stream, which unblocks any read or write that is waiting on it
func (s quicStream) Close() error {
	return s.session.CloseWithError(0, "")
}
func (s quicStream) LocalAddr() net.Addr {
	return s.session.LocalAddr()
}
func (s quicStream) RemoteAddr() net.Addr {
	return s.session.RemoteAddr()
}

func dialWtStream(ctx context.Context, endpoint string, tlsConfig *tls.Config) (wtStream, error) {
	transport := &webtransport.Transport{
		TLSClientConfig: tlsConfig,
		QUICConfig: wtQuicConfig(),
	}
	_, session, err := transport.Dial(ctx, endpoint, nil)
	transport.Close() // Note: This only stops dials that are in progress, the session lives on with its own QUIC connection
	if err != nil {
		return nil, err
	}

	stream, err := session.OpenStreamSync(ctx)
	if err != nil {
		session.CloseWithError(0, "")
		return nil, err
	}

	return quicStream{stream, session}, nil
}

// --------------------------------------------------------------------------------
// - Listener
// --------------------------------------------------------------------------------
type wtListener struct {
	server *webtransport.Server
	conn *net.UDPConn
	pendingAccepts chan Socket

	ctx context.Context // Canceled with the reason once the listener stops
	cancel context.CancelCauseFunc
}

// Note: Webtransport only listens on UDP, so it can share a port number with a TCP listener, but not with the webrtc listener, which has its own UDP socket on its port
func newWebTransportListener(c *ListenConfig) (Listener, error) {
	if c.TlsConfig == nil {
		return nil, errors.New("envoy: webtransport requires a TlsConfig")
	}

	addr, err := net.ResolveUDPAddr("udp", c.host)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	l := &wtListener{
		conn: conn,
		pendingAccepts: make(chan Socket),
		ctx: ctx,
		cancel: cancel,
	}

	originPatterns := c.OriginPatterns
	l.server = &webtransport.Server{
		H3: &http3.Server{
			TLSConfig: http3.ConfigureTLSConfig(c.TlsConfig),
			QUICConfig: wtQuicConfig(),
			Handler: http.HandlerFunc(l.serveHTTP),
		},
		CheckOrigin: func(r *http.Request) bool {
			return originAllowed(r, originPatterns)
		},
	}

	go func() {
		// Serve only returns once the socket can't be served anymore, so surface the reason through Accept
		l.cancel(l.server.Serve(conn))
	}()

	return l, nil
}

// The same rules as the websocket listener: requests without an origin (ie not from a browser) and requests from the host itself pass, everything else has to match a pattern
func originAllowed(r *http.Request, patterns []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}

	host := strings.ToLower(u.Host)
	for _, pattern := range patterns {
		matched, err := path.Match(strings.ToLower(pattern), host)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// Every session opens its stream inside its own request handler. Failures only affect that session, so they are logged rather than returned from Accept
func (l *wtListener) serveHTTP(w http.ResponseWriter, r *http.Request) {
	session, err := l.server.Upgrade(w, r)
	if err != nil {
		// Note: These are routine from scanners and bad origins
		logger.Debug().
			Err(err).
			Str("remote", r.RemoteAddr).
			Msg("envoy: webtransport upgrade failed")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	pipe, err := acceptWtPipe(l.ctx, session)
	if err != nil {
		logger.Warn().
			Err(err).
			Str("remote", r.RemoteAddr).
			Msg("envoy: webtransport session failed to open its stream")
		session.CloseWithError(0, "")
		return
	}

	select {
	case l.pendingAccepts <- newAcceptedSocket(pipe):
	case <-l.ctx.Done():
		pipe.Close()
	}
}

// Waits for the dialer to open its stream, see WebTransportDialer
func acceptWtPipe(ctx context.Context, session *webtransport.Session) (*wtPipe, error) {
	ctx, cancel := context.WithTimeout(ctx, wtOpenTimeout)
	defer cancel()

	stream, err := session.AcceptStream(ctx)
	if err != nil {
		return nil, err
	}
	pipe := newWtPipe(quicStream{stream, session})

	// Consume the empty message that the dialer opened the stream with
	deadline, _ := ctx.Deadline()
	stream.SetReadDeadline(deadline)
	_, err = pipe.Read(nil)
	if err != nil {
		return nil, err
	}
	stream.SetReadDeadline(time.Time{})

	return pipe, nil
}

func (l *wtListener) Accept() (Socket, error) {
	select {
	case sock := <-l.pendingAccepts:
		return sock, nil
	case <-l.ctx.Done():
		return nil, context.Cause(l.ctx)
	}
}

// Note: This also closes every connection that was accepted from this listener
func (l *wtListener) Close() error {
	l.cancel(net.ErrClosed)
	return errors.Join(l.server.Close(), l.conn.Close())
}

func (l *wtListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}
