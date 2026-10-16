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
	"sync"
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

// A connection whose unsent writes grow past this is treated as dead
const wtMaxPendingWrite = 4 << 20

var errWtWriteBacklog = errors.New("envoy: webtransport connection fell too far behind on writes")

// Writes are queued and sent by a writer goroutine, so that Write never waits on the network
// Note: quic-go's Write waits whenever the congestion window is full, so without the queue every network hiccup stalls the writer (on a client, that's usually the frame). See runWriter
type quicStream struct {
	*webtransport.Stream
	session *webtransport.Session

	writeMu sync.Mutex
	pending []byte // Written, but not yet taken by the writer
	writeErr error // Set once the writer fails, after which every write fails
	pendingSignal chan struct{} // Wakes the writer when there is something pending
}

func newQuicStream(stream *webtransport.Stream, session *webtransport.Session) *quicStream {
	s := &quicStream{
		Stream: stream,
		session: session,
		pendingSignal: make(chan struct{}, 1),
	}
	go s.runWriter()
	return s
}

// Queues b to be sent, in order with every other write
func (s *quicStream) Write(b []byte) (int, error) {
	s.writeMu.Lock()
	if s.writeErr != nil {
		err := s.writeErr
		s.writeMu.Unlock()
		return 0, err
	}
	if len(s.pending) + len(b) > wtMaxPendingWrite {
		s.writeMu.Unlock()
		s.Close()
		return 0, errWtWriteBacklog
	}
	s.pending = append(s.pending, b...)
	s.writeMu.Unlock()

	select {
	case s.pendingSignal <- struct{}{}:
	default: // The writer is already signaled
	}
	return len(b), nil
}

// Sends everything that is pending, in one write, until the session closes
// Note: The two buffers swap roles each time, so nothing is allocated once they have grown
func (s *quicStream) runWriter() {
	var sending []byte
	for {
		select {
		case <-s.pendingSignal:
		case <-s.session.Context().Done():
			s.setWriteErr(net.ErrClosed)
			return
		}

		s.writeMu.Lock()
		sending, s.pending = s.pending, sending[:0]
		s.writeMu.Unlock()
		if len(sending) == 0 { continue }

		_, err := s.Stream.Write(sending)
		if err != nil {
			s.setWriteErr(err)

			// Note: The read side then fails too, which is how the socket finds out
			s.Close()
			return
		}
	}
}

// Makes every write from now on fail with err, because nothing is sending them anymore
func (s *quicStream) setWriteErr(err error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.writeErr = err
	s.pending = nil
}

// Note: Closing the session also closes its stream, which unblocks any read or write that is waiting on it
func (s *quicStream) Close() error {
	return s.session.CloseWithError(0, "")
}
func (s *quicStream) LocalAddr() net.Addr {
	return s.session.LocalAddr()
}
func (s *quicStream) RemoteAddr() net.Addr {
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

	return newQuicStream(stream, session), nil
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
	pipe := newWtPipe(newQuicStream(stream, session))

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
