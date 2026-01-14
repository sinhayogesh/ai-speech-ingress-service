package main

import (
	"context"
	"encoding/binary"
	"flag"
	"io"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "ai-speech-ingress-service/proto"
)

// WAV header is 44 bytes for standard PCM files
const wavHeaderSize = 44

// Stream audio in chunks to simulate real-time streaming
// At 8kHz 16-bit mono = 16000 bytes/second
// 100ms chunks = 1600 bytes
const chunkSize = 1600
const baseChunkIntervalMs = 100

func main() {
	audioFile := flag.String("audio", "../testdata/sample-8khz.wav", "Path to WAV file (8kHz 16-bit mono)")
	serverAddr := flag.String("server", "localhost:50051", "gRPC server address")
	interactionId := flag.String("interaction", "test-audio-"+time.Now().Format("150405"), "Interaction ID")
	tenantId := flag.String("tenant", "tenant-demo", "Tenant ID")
	slowdown := flag.Float64("slow", 1.0, "Slowdown factor (1.0 = realtime, 2.0 = half speed, etc)")
	flag.Parse()

	// Calculate chunk interval based on slowdown factor
	chunkInterval := time.Duration(float64(baseChunkIntervalMs)**slowdown) * time.Millisecond

	// Open audio file
	f, err := os.Open(*audioFile)
	if err != nil {
		log.Fatalf("Failed to open audio file: %v", err)
	}
	defer f.Close()

	// Read and validate WAV header
	header := make([]byte, wavHeaderSize)
	if _, err := io.ReadFull(f, header); err != nil {
		log.Fatalf("Failed to read WAV header: %v", err)
	}

	// Validate it's a WAV file
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		log.Fatal("Not a valid WAV file")
	}

	// Extract audio format info
	audioFormat := binary.LittleEndian.Uint16(header[20:22])
	numChannels := binary.LittleEndian.Uint16(header[22:24])
	sampleRate := binary.LittleEndian.Uint32(header[24:28])
	bitsPerSample := binary.LittleEndian.Uint16(header[34:36])

	log.Printf("WAV file: format=%d channels=%d sampleRate=%d bitsPerSample=%d",
		audioFormat, numChannels, sampleRate, bitsPerSample)

	if audioFormat != 1 { // PCM
		log.Fatal("Only PCM format supported")
	}
	if sampleRate != 8000 {
		log.Printf("Warning: Sample rate is %d Hz, expected 8000 Hz", sampleRate)
	}

	// Connect to gRPC server
	conn, err := grpc.NewClient(*serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	log.Printf("Connected to %s", *serverAddr)

	client := pb.NewAudioStreamServiceClient(conn)

	// Create stream with longer timeout for real audio (account for slowdown)
	timeout := time.Duration(90+int(50**slowdown)) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	stream, err := client.StreamAudio(ctx)
	if err != nil {
		log.Fatalf("Failed to create stream: %v", err)
	}

	log.Printf("Streaming audio: interactionId=%s tenantId=%s (slowdown=%.1fx)", *interactionId, *tenantId, *slowdown)

	// Stream audio in chunks
	audioChunk := make([]byte, chunkSize)
	var totalBytes int64
	var chunkNum int
	startTime := time.Now()

	for {
		n, err := f.Read(audioChunk)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Failed to read audio: %v", err)
		}

		chunkNum++
		totalBytes += int64(n)
		offsetMs := int64(chunkNum * baseChunkIntervalMs) // Audio offset based on real audio time

		frame := &pb.AudioFrame{
			InteractionId: *interactionId,
			TenantId:      *tenantId,
			Audio:         audioChunk[:n],
			AudioOffsetMs: offsetMs,
		}

		if err := stream.Send(frame); err != nil {
			log.Fatalf("Failed to send frame: %v", err)
		}

		if chunkNum%10 == 0 {
			log.Printf("Sent chunk %d (%d bytes total, offset=%dms)", chunkNum, totalBytes, offsetMs)
		}

		// Simulate real-time streaming (with optional slowdown)
		time.Sleep(chunkInterval)
	}

	elapsed := time.Since(startTime)
	log.Printf("Finished streaming: %d chunks, %d bytes in %v", chunkNum, totalBytes, elapsed)

	// Wait for Google STT to finish processing buffered audio
	// Google needs time to transcribe remaining utterances after audio ends
	log.Println("Waiting for STT to finish processing...")
	time.Sleep(10 * time.Second)

	// Close stream and wait for response
	log.Println("Closing stream...")

	ack, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("Failed to receive ack: %v", err)
	}

	log.Printf("✅ Stream completed: interactionId=%s", ack.InteractionId)
}
