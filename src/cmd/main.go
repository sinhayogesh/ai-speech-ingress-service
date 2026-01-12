package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	grpcapi "ai-speech-ingress-service/internal/api/grpc"
	"ai-speech-ingress-service/internal/config"
	"ai-speech-ingress-service/internal/events"
)

func main() {
	cfg := config.Load()

	// Create Kafka publisher with separate topics for partial and final transcripts
	publisher := events.New(&events.Config{
		Enabled:      cfg.Kafka.Enabled,
		Brokers:      cfg.Kafka.Brokers,
		TopicPartial: cfg.Kafka.TopicPartial,
		TopicFinal:   cfg.Kafka.TopicFinal,
		Principal:    cfg.Kafka.Principal,
	})
	defer publisher.Close()

	lis, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	server := grpc.NewServer()

	// Register gRPC health check service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("ai.speech.ingress.AudioStreamService", grpc_health_v1.HealthCheckResponse_SERVING)

	// Register application services
	grpcapi.Register(server, publisher, cfg.STTProvider)

	// Enable gRPC reflection for debugging tools like grpcurl
	reflection.Register(server)

	go func() {
		log.Printf("Speech Ingress Service started on :%s", cfg.Port)
		if err := server.Serve(lis); err != nil {
			log.Fatalf("grpc serve failed: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("shutting down gRPC server")
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	server.GracefulStop()
}
