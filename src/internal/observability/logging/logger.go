// Package logging provides structured logging with zerolog.
package logging

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Config holds logging configuration.
type Config struct {
	Level      string // debug, info, warn, error
	Format     string // json, console
	TimeFormat string // RFC3339, Unix, etc.
}

// DefaultConfig returns sensible default logging configuration.
func DefaultConfig() Config {
	return Config{
		Level:      "info",
		Format:     "json",
		TimeFormat: time.RFC3339,
	}
}

// Init initializes the global zerolog logger.
func Init(cfg Config) {
	// Set time format
	zerolog.TimeFieldFormat = cfg.TimeFormat

	// Parse log level
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Configure output format
	var output io.Writer = os.Stdout
	if cfg.Format == "console" {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.Kitchen,
		}
	}

	// Set global logger
	log.Logger = zerolog.New(output).
		With().
		Timestamp().
		Caller().
		Logger()
}

// Logger returns a new logger with common fields for the service.
func Logger() zerolog.Logger {
	return log.Logger
}

// WithInteraction returns a logger with interaction context.
func WithInteraction(interactionId, tenantId string) zerolog.Logger {
	return log.With().
		Str("interactionId", interactionId).
		Str("tenantId", tenantId).
		Logger()
}

// WithSegment returns a logger with segment context.
func WithSegment(interactionId, tenantId, segmentId string) zerolog.Logger {
	return log.With().
		Str("interactionId", interactionId).
		Str("tenantId", tenantId).
		Str("segmentId", segmentId).
		Logger()
}

// WithStream returns a logger with stream context.
func WithStream(interactionId, tenantId, segmentId, provider string) zerolog.Logger {
	return log.With().
		Str("interactionId", interactionId).
		Str("tenantId", tenantId).
		Str("segmentId", segmentId).
		Str("sttProvider", provider).
		Logger()
}

// WithComponent returns a logger with a component tag.
func WithComponent(component string) zerolog.Logger {
	return log.With().
		Str("component", component).
		Logger()
}
