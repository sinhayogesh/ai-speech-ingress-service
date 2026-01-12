package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	grpcapi "ai-speech-ingress-service/internal/api/grpc"
	"ai-speech-ingress-service/internal/config"
)

func main() {
	cfg := config.Load()

	lis, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	server := grpc.NewServer()
	grpcapi.Register(server)

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
	server.GracefulStop()
}
