package net

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// By default use the global logger
var logger = log.Logger

func SetLogger(newLogger zerolog.Logger) {
	logger = newLogger
}
