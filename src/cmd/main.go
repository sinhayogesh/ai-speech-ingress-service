package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ai-speech-ingress-service/internal/app"
	"ai-speech-ingress-service/internal/config"
	httpTransport "ai-speech-ingress-service/internal/http"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339

	logger := log.With().
		Str("service", "ai-speech-ingress-service").
		Str("component", "main").
		Logger()

	logger.Info().Msg("Starting AI Speech Ingress service")

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load configuration")
	}

	application := app.New(cfg)
	if err := application.Start(); err != nil {
		logger.Fatal().Err(err).Msg("Application failed to start")
	}

	router := httpTransport.NewRouter(application)

	server := &http.Server{
		Addr:    cfg.HTTPListenAddress,
		Handler: router,
	}

	// Start HTTP server
	go func() {
		logger.Info().Str("addr", cfg.HTTPListenAddress).Msg("HTTP server listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	// Start health server
	go startHealthServer(logger, cfg)

	// Wait for shutdown signal
	waitForShutdown(logger, server, application)
}

func startHealthServer(logger zerolog.Logger, cfg *config.Configuration) {
	logger.Info().Str("addr", cfg.HealthListenAddress).Msg("Health server listening")
	if err := http.ListenAndServe(cfg.HealthListenAddress, nil); err != nil && err != http.ErrServerClosed {
		logger.Fatal().Err(err).Msg("Health server failed")
	}
}

func waitForShutdown(logger zerolog.Logger, server *http.Server, application *app.Application) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigs
	logger.Info().Str("signal", sig.String()).Msg("Shutdown signal received")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("HTTP server forced to shutdown")
	} else {
		logger.Info().Msg("HTTP server shutdown complete")
	}

	application.Shutdown()
}

