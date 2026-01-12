package app

import (
	"os"
	"strings"
	"time"

	"ai-speech-ingress-service/internal/config"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Application holds process-wide state for the service.
type Application struct {
	StartupTime time.Time
	Logger      zerolog.Logger
	Cfg         *config.Configuration
}

// New constructs a new Application from the provided configuration.
func New(cfg *config.Configuration) *Application {
	a := &Application{
		Cfg: cfg,
	}
	a.setupLogger()

	appLogger := a.Logger.With().
		Str("component", "application").
		Str("method", "New").
		Logger()

	appLogger.Info().Msg("AI Speech Ingress service application created")
	return a
}

// setupLogger configures zerolog for the service.
func (a *Application) setupLogger() {
	logLevel := zerolog.InfoLevel // Default
	if envLevel := os.Getenv("ZEROLOG_LOG_LEVEL"); envLevel != "" {
		if parsedLevel, err := zerolog.ParseLevel(strings.ToLower(envLevel)); err == nil {
			logLevel = parsedLevel
		}
	}

	zerolog.SetGlobalLevel(logLevel)
	zerolog.TimeFieldFormat = time.RFC3339

	if os.Getenv("ENV") == "dev" {
		a.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
			With().
			Timestamp().
			Str("service", "ai-speech-ingress-service").
			Str("component", "application").
			Logger()
	} else {
		a.Logger = zerolog.New(os.Stdout).With().
			Timestamp().
			Str("service", "ai-speech-ingress-service").
			Str("component", "application").
			Logger()
	}

	a.Logger.Info().
		Str("logLevel", logLevel.String()).
		Str("environment", os.Getenv("ENV")).
		Msg("Logger setup completed")
}

// Start performs any startup work required before serving traffic.
func (a *Application) Start() error {
	startLogger := a.Logger.With().
		Str("method", "Start").
		Logger()

	a.StartupTime = time.Now().UTC()
	startLogger.Info().
		Time("startupTime", a.StartupTime).
		Msg("AI Speech Ingress service starting")

	return nil
}

// Shutdown performs a best-effort cleanup before process exit.
func (a *Application) Shutdown() {
	shutdownLogger := a.Logger.With().
		Str("method", "Shutdown").
		Logger()

	shutdownLogger.Info().Msg("AI Speech Ingress service shutting down")
}

