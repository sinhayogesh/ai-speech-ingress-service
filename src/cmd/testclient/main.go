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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := client.StreamAudio(ctx)
	if err != nil {
		log.Fatalf("failed to create stream: %v", err)
	}

	// Send more audio frames to trigger utterance boundary detection
	// The mock adapter needs 4+ frames to complete an utterance:
	// Frame 1-3: partials ("I want", "I want to", "I want to cancel")
	// Frame 4+: triggers final transcript + end-of-utterance
	numFrames := 6
	for i := 1; i <= numFrames; i++ {
		frame := &pb.AudioFrame{
			InteractionId: "int-123",
			TenantId:      "tenant-456",
			Audio:         []byte("audio-chunk-" + string(rune('0'+i))),
			AudioOffsetMs: int64(i * 100),
		}
		log.Printf("Sending frame %d/%d: interactionId=%s", i, numFrames, frame.InteractionId)
		if err := stream.Send(frame); err != nil {
			log.Fatalf("failed to send frame: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Signal end of stream
	log.Println("Closing stream...")

	// Close and receive response
	ack, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("failed to receive ack: %v", err)
	}

	log.Printf("Received ack: interactionId=%s", ack.InteractionId)
}
