package main

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "ai-speech-ingress-service/proto"
)

func main() {
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	log.Println("Connected to server")

	client := pb.NewAudioStreamServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.StreamAudio(ctx)
	if err != nil {
		log.Fatalf("failed to create stream: %v", err)
	}

	// Send audio frames
	frames := []*pb.AudioFrame{
		{InteractionId: "int-123", TenantId: "tenant-456", Audio: []byte("audio-chunk-1")},
		{InteractionId: "int-123", TenantId: "tenant-456", Audio: []byte("audio-chunk-2")},
		{InteractionId: "int-123", TenantId: "tenant-456", Audio: []byte("audio-chunk-3"), EndOfUtterance: true},
	}

	for _, frame := range frames {
		log.Printf("Sending frame: interactionId=%s", frame.InteractionId)
		if err := stream.Send(frame); err != nil {
			log.Fatalf("failed to send frame: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Close and receive response
	ack, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("failed to receive ack: %v", err)
	}

	log.Printf("Received ack: interactionId=%s", ack.InteractionId)
}

