package envoy

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/unitoftime/envoy/net"
)

// By default use the global logger
var logger = log.Logger

func SetLogger(newLogger zerolog.Logger) {
	logger = newLogger
	net.SetLogger(newLogger)
}
