package net

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/unitoftime/rtcnet"
)

type WebRtcDialer struct {
	Url string
	TlsConfig *tls.Config
	Ordered bool
	IceServers []string
	WebsocketFallback bool
}
func (d WebRtcDialer) DialPipe() (Pipe, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20 * time.Second)
	defer cancel()

	rtcPipe := make(chan Pipe)
	go func () {
		_, host := parseSchemeHost(d.Url)

		maxAttempts := 2
		for i := 0; i < maxAttempts; i++ {
			conn, err := rtcnet.Dial(host, d.TlsConfig, d.Ordered, d.IceServers)
			if err == nil {
				rtcPipe <- pipeWrapper{conn, "webrtc"}
				return
			} else {
				logger.Error().Err(err).Int("Attempt", i).Msg("Failed to dial webrtc")
			}
		}

		cancel()
	}()

	select {
	case pipe := <-rtcPipe:
		return pipe, nil
	case <-ctx.Done():
		// Do nothing, just finish
	}

	// Fallback to websockets if available
	if d.WebsocketFallback {
		logger.Error().Msg("failed to dial webrtc. fallback to wss")
		fallbackPath := strings.ReplaceAll(d.Url, "webrtc", "wss")
		fallbackPath = fallbackPath + "/wss"
		wsConn, wsErr := dialWebsocket(fallbackPath, d.TlsConfig)
		return wsConn, wsErr
	}

	return nil, errors.New("failed to dial")


	// _, host := parseSchemeHost(d.Url)
	// conn, err := rtcnet.Dial(host, d.TlsConfig, d.Ordered, d.IceServers)
	// if err == nil {
	// 	return pipeWrapper{conn, "webrtc"}, nil
	// }

	// // Retry once
	// conn, err = rtcnet.Dial(host, d.TlsConfig, d.Ordered, d.IceServers)
	// if err == nil {
	// 	return pipeWrapper{conn, "webrtc"}, nil
	// }

	// // Fallback to websockets if available
	// if d.WebsocketFallback {
	// 	// logger.Error().
	// 	// 	Err(err).
	// 	// 	Msg("Failed to dial webrtc. Trying websockets")
	// 	fallbackPath := strings.ReplaceAll(d.Url, "webrtc", "wss")
	// 	fallbackPath = fallbackPath + "/wss"
	// 	wsConn, wsErr := dialWebsocket(fallbackPath, d.TlsConfig)
	// 	return wsConn, wsErr
	// }
	// return pipeWrapper{conn, "wss"}, err
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
