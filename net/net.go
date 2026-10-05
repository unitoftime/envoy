package net

import (
	"context"
	"errors"
	"fmt"
	"time"

	"crypto/tls"
	"net"
	"net/http"
	"net/url"
)

// TODO - Ensure sent messages remain under this
// Calculation: 1460 Byte = 1500 Byte - 20 Byte IP Header - 20 Byte TCP Header
// const MaxMsgSize = 1460 // bytes
const MaxRecvMsgSize = 64 * 1024 // 8 KB // TODO! - this is arbitrary. Need a better way to manage message sizes. I'm just setting this to be big enough for my mmo

var ErrNetwork = errors.New("network error")
var ErrDisconnected = errors.New("socket disconnected")
var ErrClosed = errors.New("socket closed") // Indicates that the socket is closed. Currently if you get this error then it means the socket will never receive or send again!

type Listener interface {
	// Accept waits for and returns the next connection to the listener.
	Accept() (Socket, error)

	// Close closes the listener.
	// Any blocked Accept operations will be unblocked and return errors.
	Close() error

	// Addr returns the listener's network address.
	Addr() net.Addr
}

// TODO: This is basically a net.Conn
type Pipe interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error

	// LocalAddr returns the local network address, if known.
	LocalAddr() net.Addr

	// RemoteAddr returns the remote network address, if known.
	RemoteAddr() net.Addr

	// SetReadTimeout(time.Duration)
	// SetWriteTimeout(time.Duration)
	Transport() string
}

type pipeWrapper struct {
	net.Conn
	transport string
}
func (p pipeWrapper) Transport() string {
	return p.transport
}

type Socket interface {
	// TODO - SetReadDeadline and SetWriteDeadline could be nice to have!

	Read([]byte) (int, error)
	Write([]byte) (int, error)

	Close() error

	Connected() bool
	Closed() bool
	// Wait() // Wait for the connection to stabalize

	// Reads and clears the number egress bytes
	ReadEgress() int64
	// Reads and clears the number ingress bytes
	ReadIngress() int64
	Transport() string

	// LocalAddr returns the local network address, if known.
	LocalAddr() net.Addr

	// RemoteAddr returns the remote network address, if known.
	RemoteAddr() net.Addr
}

// --------------------------------------------------------------------------------
// - Dialer
// --------------------------------------------------------------------------------
func NewDialSocket(dialer Dialer) Socket {
	sock := newDialSocket(dialer)
	sock.triggerRedial(1 * time.Nanosecond)
	return sock
}

type Dialer interface {
	// Dials a new pipe, giving up once the context is done. The context only bounds the dial, the returned pipe outlives it
	DialPipe(context.Context) (Pipe, error)
}

// Dials with each attempt in order, and returns the first pipe that connects
type FallbackDialer []DialAttempt

type DialAttempt struct {
	Dialer Dialer
	Timeout time.Duration // How long this attempt gets to connect before we move on to the next one
}

func (d FallbackDialer) DialPipe(ctx context.Context) (Pipe, error) {
	var errs []error
	for i, attempt := range d {
		attemptCtx, cancel := context.WithTimeout(ctx, attempt.Timeout)
		pipe, err := attempt.Dialer.DialPipe(attemptCtx)
		cancel()
		if err == nil {
			return pipe, nil
		}

		logger.Warn().Err(err).Int("Attempt", i).Msg("Envoy.FallbackDialer failed to dial")
		errs = append(errs, err)

		if ctx.Err() != nil {
			break // We are out of time for every attempt
		}
	}

	return nil, errors.Join(errs...)
}

// TODO: Just pass host in directly instead of scheme (so we dont have to use this func)
func parseSchemeHost(urlString string) (string, string) {
	// Parse the config
	u, err := url.Parse(urlString)
	if err != nil {
		// TODO - wrap this up in the creation of the dialconfig
		panic(fmt.Sprintf("URL Parsing Error: %v", err))
	}
	return u.Scheme, u.Host
}

// --------------------------------------------------------------------------------
// - Listener
// --------------------------------------------------------------------------------
// For listening for sockets
type ListenConfig struct {
	Url string   // Note: We only use the [scheme]://[host] portion of this
	TlsConfig *tls.Config

	HttpServer *http.Server // TODO - For Websockets only, maybe split up? - Note we have to wrap their Handler with our own handler!
	OriginPatterns []string
	IceServers []string
	PublicIP string // WebRTC only: the IPv4 address clients reach the listener at. See rtcnet.ListenConfig
	IceLite bool // WebRTC only: requires IceServers to be empty. See rtcnet.ListenConfig

	// These are generated based on the upper config
	scheme string
	host string
}

func (c *ListenConfig) Listen() (Listener, error) {
	u, err := url.Parse(c.Url)
	if err != nil {
		// TODO - wrap this up in the creation of the dialconfig
		panic(fmt.Sprintf("URL Parsing Error: %v", err))
	}
	c.scheme = u.Scheme
	c.host = u.Host

	if c.scheme == "tcp" || c.scheme == "tcp4" || c.scheme == "tcp6" || c.scheme == "unix" || c.scheme == "unixpacket" {
		return newTcpListener(c)
	} else if c.scheme == "wss" {
		return newWebsocketListener(c)
	} else if c.scheme == "webrtc" {
		return newWebRtcListener(c)
	} else if c.scheme == "webtransport" {
		return newWebTransportListener(c)
	} else if c.scheme == "ws" {
		panic("Not implemented yet")
	} else {
		panic("Unsupported network")
	}
}
