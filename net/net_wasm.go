//go:build js
// +build js

package net

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/coder/websocket"
)

func dialWs(ctx context.Context, url string, tlsConfig *tls.Config) (*websocket.Conn, error) {
	wsConn, _, err := websocket.Dial(ctx, url, nil)
	return wsConn, err
}

const redialHackDur = 1 * time.Second
