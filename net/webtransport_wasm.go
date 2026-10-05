//go:build js
// +build js

package net

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall/js"
)

func newWebTransportListener(c *ListenConfig) (Listener, error) {
	return nil, errors.New("envoy: listening for webtransport is not supported in the browser")
}

// Note: The browser doesn't tell us the addresses of a session
type wtAddr string
func (a wtAddr) Network() string {
	return "webtransport"
}
func (a wtAddr) String() string {
	return string(a)
}

// Blocks until the promise settles
func await(promise js.Value) (js.Value, error) {
	type result struct {
		val js.Value
		err error
	}
	done := make(chan result, 1)

	resolve := js.FuncOf(func(this js.Value, args []js.Value) any {
		done <- result{val: args[0]}
		return nil
	})
	defer resolve.Release()
	reject := js.FuncOf(func(this js.Value, args []js.Value) any {
		// Note: The reason is usually an error object, but it can be any value, so let JS stringify it
		reason := js.Global().Call("String", args[0]).String()
		done <- result{err: errors.New("envoy: webtransport: " + reason)}
		return nil
	})
	defer reject.Release()

	promise.Call("then", resolve, reject)
	res := <-done
	return res.val, res.err
}

// The browser's WebTransport session, along with the reader and writer of its one stream
type jsStream struct {
	session js.Value
	reader, writer js.Value
	addr wtAddr

	unread js.Value // The rest of the last chunk the reader gave us, which is rarely the size that Read was asked for
	unreadLen int
}

func dialWtStream(ctx context.Context, url string, tlsConfig *tls.Config) (wtStream, error) {
	session, err := newJsWebTransport(url)
	if err != nil {
		return nil, err
	}

	// Closing the session rejects whichever promise we are waiting on
	stop := context.AfterFunc(ctx, func() {
		session.Call("close")
	})
	defer stop()

	_, err = await(session.Get("ready"))
	if err != nil {
		return nil, err
	}

	stream, err := await(session.Call("createBidirectionalStream"))
	if err != nil {
		session.Call("close")
		return nil, err
	}

	if !stop() {
		return nil, context.Cause(ctx) // We ran out of time right as we finished, so the session is already closing
	}

	return &jsStream{
		session: session,
		reader: stream.Get("readable").Call("getReader"),
		writer: stream.Get("writable").Call("getWriter"),
		addr: wtAddr(url),
	}, nil
}

// Note: The constructor throws rather than rejecting, ie when the page isn't allowed to connect to the url
func newJsWebTransport(url string) (session js.Value, err error) {
	constructor := js.Global().Get("WebTransport")
	if constructor.IsUndefined() {
		return js.Value{}, errors.New("envoy: this browser doesn't support webtransport")
	}

	defer func() {
		r := recover()
		if r != nil {
			err = fmt.Errorf("envoy: webtransport: %v", r)
		}
	}()
	return constructor.New(url), nil
}

func (s *jsStream) Read(b []byte) (int, error) {
	for s.unreadLen == 0 {
		res, err := await(s.reader.Call("read"))
		if err != nil {
			return 0, err
		}
		if res.Get("done").Bool() {
			return 0, io.EOF
		}

		s.unread = res.Get("value")
		s.unreadLen = s.unread.Get("byteLength").Int()
	}

	n := js.CopyBytesToGo(b, s.unread)
	s.unreadLen -= n
	if s.unreadLen > 0 {
		s.unread = s.unread.Call("subarray", n)
	}
	return n, nil
}

// Swallows the rejection of a promise that we don't wait on, which the browser would otherwise report as an uncaught error
var jsIgnoreRejection = js.FuncOf(func(this js.Value, args []js.Value) any {
	return nil
})

// Note: We hand the bytes to the browser without waiting for it to take them, the same as a websocket send. Waiting would yield to the browser's event loop in the middle of whatever the caller is doing, ie a frame
func (s *jsStream) Write(b []byte) (int, error) {
	// The browser reports no desired size once the stream has failed, which is how we find out that an earlier write was rejected
	if s.writer.Get("desiredSize").IsNull() {
		return 0, errors.New("envoy: webtransport: stream is closed")
	}

	chunk := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(chunk, b)
	s.writer.Call("write", chunk).Call("catch", jsIgnoreRejection)

	return len(b), nil
}

// Note: Closing the session also errors its stream, which unblocks any read or write that is waiting on it
func (s *jsStream) Close() error {
	s.session.Call("close")
	return nil
}

func (s *jsStream) LocalAddr() net.Addr {
	return s.addr
}
func (s *jsStream) RemoteAddr() net.Addr {
	return s.addr
}
