package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	grpcapi "ai-speech-ingress-service/internal/api/grpc"
	"ai-speech-ingress-service/internal/config"
	"ai-speech-ingress-service/internal/events"
	"ai-speech-ingress-service/internal/observability"
	"ai-speech-ingress-service/internal/observability/logging"
	"ai-speech-ingress-service/internal/observability/metrics"
	"ai-speech-ingress-service/internal/service/audio"
)

func main() {
	cfg := config.Load()

	// Initialize structured logging
	logging.Init(logging.Config{
		Level:  cfg.Observability.LogLevel,
		Format: cfg.Observability.LogFormat,
	})

	log.Info().
		Str("servicePrincipal", cfg.Service.Principal).
		Str("grpcPort", cfg.Service.GRPCPort).
		Str("metricsPort", cfg.Observability.MetricsPort).
		Str("logLevel", cfg.Observability.LogLevel).
		Msg("Starting Speech Ingress Service")

	log.Info().
		Str("provider", cfg.STT.Provider).
		Str("languageCode", cfg.STT.LanguageCode).
		Int("sampleRateHz", cfg.STT.SampleRateHz).
		Bool("interimResults", cfg.STT.InterimResults).
		Str("audioEncoding", cfg.STT.AudioEncoding).
		Msg("STT configuration")

	log.Info().
		Int64("maxAudioBytes", cfg.SegmentLimits.MaxAudioBytes).
		Dur("maxDuration", cfg.SegmentLimits.MaxDuration).
		Int("maxPartials", cfg.SegmentLimits.MaxPartials).
		Msg("Segment limits")

	log.Info().
		Bool("kafkaEnabled", cfg.Kafka.Enabled).
		Msg("Kafka configuration")

	// Start observability HTTP server (Prometheus metrics)
	var obsServer *observability.Server
	if cfg.Observability.MetricsEnabled {
		obsServer = observability.NewServer(":" + cfg.Observability.MetricsPort)
		obsServer.Start()
	}

	// Create Kafka publisher with separate topics for partial and final transcripts
	publisher := events.New(&events.Config{
		Enabled:      cfg.Kafka.Enabled,
		Brokers:      cfg.Kafka.Brokers,
		TopicPartial: cfg.Kafka.TopicPartial,
		TopicFinal:   cfg.Kafka.TopicFinal,
		Principal:    cfg.Kafka.Principal,
	})
	defer publisher.Close()

	lis, err := net.Listen("tcp", ":"+cfg.Service.GRPCPort)
	if err != nil {
		log.Fatal().Err(err).Str("port", cfg.Service.GRPCPort).Msg("Failed to listen")
	}

	// Create gRPC server with observability interceptors
	m := metrics.DefaultMetrics
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			observability.UnaryServerInterceptor(),
		),
		grpc.ChainStreamInterceptor(
			observability.StreamServerInterceptor(m),
		),
	)

	// Register gRPC health check service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("ai.speech.ingress.AudioStreamService", grpc_health_v1.HealthCheckResponse_SERVING)

	// Register application services with STT config and segment limits
	limits := audio.SegmentLimits{
		MaxAudioBytes: cfg.SegmentLimits.MaxAudioBytes,
		MaxDuration:   cfg.SegmentLimits.MaxDuration,
		MaxPartials:   cfg.SegmentLimits.MaxPartials,
	}
	grpcapi.RegisterWithConfig(server, publisher, cfg.STT, limits)

	// Enable gRPC reflection for debugging tools like grpcurl
	reflection.Register(server)

	go func() {
		log.Info().
			Str("port", cfg.Service.GRPCPort).
			Msg("gRPC server listening")
		if err := server.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("gRPC serve failed")
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Info().Msg("Received shutdown signal")

	// Graceful shutdown
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	// Shutdown observability server
	if obsServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := obsServer.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("Error shutting down observability server")
		}
	}

	server.GracefulStop()
	log.Info().Msg("Server stopped")
}
