package net

import (
	"fmt"
	"errors"
	"time"

	"net"
	"net/url"
	"net/http"
	"crypto/tls"
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

	// SetReadTimeout(time.Duration)
	// SetWriteTimeout(time.Duration)
}

type Socket interface {
	// TODO - SetReadDeadline and SetWriteDeadline could be nice to have!

	Read([]byte) (int, error)
	Write([]byte) (int, error)

	Close() error

	Connected() bool
	Closed() bool
	// Wait() // Wait for the connection to stabalize
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
	DialPipe() (Pipe, error)
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
	} else if c.scheme == "ws" {
		panic("Not implemented yet")
	} else {
		panic("Unsupported network")
	}
}
