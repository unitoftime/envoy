package net

import (
	"context"
	"net"
)

type TcpDialer struct {
	Url string
	// TlsConfig *tls.Config
}
func (d TcpDialer) DialPipe(ctx context.Context) (Pipe, error) {
	_, host := parseSchemeHost(d.Url)
	return dialTcp(ctx, host)
}

// A tcp connection, framed into messages
type tcpPipe struct {
	conn net.Conn
	framer *framer
}
func newTcpPipe(conn net.Conn) *tcpPipe {
	return &tcpPipe{
		conn: conn,
		framer: newFramer(conn),
	}
}

func dialTcp(ctx context.Context, host string) (*tcpPipe, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}

	return newTcpPipe(conn), nil
}

func (t *tcpPipe) Transport() string {
	return "tcp"
}
func (s *tcpPipe) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}
func (s *tcpPipe) RemoteAddr() net.Addr {
	return s.conn.RemoteAddr()
}

func (t *tcpPipe) Read(b []byte) (int, error) {
	return t.framer.Read(b)
}

func (t *tcpPipe) Write(b []byte) (int, error) {
	return t.framer.Write(b)
}

func (t *tcpPipe) Close() error {
	return t.conn.Close()
}

type TcpListener struct {
	listener net.Listener
}
func newTcpListener(c *ListenConfig) (*TcpListener, error) {
	listener, err := net.Listen(c.scheme, c.host)
	if err != nil {
		return nil, err
	}
	sockListener := &TcpListener{
		listener: listener,
	}
	return sockListener, nil
}

func (l *TcpListener) Accept() (Socket, error) {
	c, err := l.listener.Accept()
	if err != nil {
		return nil, err
	}

	return newAcceptedSocket(newTcpPipe(c)), nil
}
func (l *TcpListener) Close() error {
	return l.listener.Close()
}
func (l *TcpListener) Addr() net.Addr {
	return l.listener.Addr()
}

