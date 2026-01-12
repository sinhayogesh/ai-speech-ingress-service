// Package grpcapi provides the gRPC server implementation for the AI Speech Ingress service.
package grpcapi

import (
	"context"
	"io"
	"log"

	"google.golang.org/grpc"

	"ai-speech-ingress-service/internal/events"
	"ai-speech-ingress-service/internal/schema"
	"ai-speech-ingress-service/internal/service/audio"
	"ai-speech-ingress-service/internal/service/segment"
	"ai-speech-ingress-service/internal/service/stt"
	"ai-speech-ingress-service/internal/service/stt/google"
	"ai-speech-ingress-service/internal/service/stt/mock"
	pb "ai-speech-ingress-service/proto"
)

// Server implements the AudioStreamService gRPC service.
type Server struct {
	pb.UnimplementedAudioStreamServiceServer
	segments    *segment.Generator
	publisher   *events.Publisher
	validator   *schema.Validator
	sttProvider string
}

// Register creates a new Server and registers it with the gRPC server.
func Register(g *grpc.Server, publisher *events.Publisher, sttProvider string) {
	s := &Server{
		segments:    segment.New(),
		publisher:   publisher,
		validator:   schema.New(),
		sttProvider: sttProvider,
	}
	log.Printf("Using STT provider: %s", sttProvider)
	pb.RegisterAudioStreamServiceServer(g, s)
}

// StreamAudio handles bidirectional audio streaming for speech-to-text transcription.
// It receives audio frames from the client, forwards them to the STT provider,
// and publishes transcript events (partial and final) to the event bus.
func (s *Server) StreamAudio(stream pb.AudioStreamService_StreamAudioServer) error {
	ctx := stream.Context()

	// Read first frame to extract metadata (interactionId, tenantId)
	frame, err := stream.Recv()
	if err != nil {
		return err
	}

	interactionId := frame.InteractionId
	tenantId := frame.TenantId
	segmentId := s.segments.Next(interactionId)

	log.Printf("Starting stream: interactionId=%s tenantId=%s segmentId=%s", interactionId, tenantId, segmentId)

	// Create and initialize STT adapter
	adapter, err := s.createSTTAdapter(ctx)
	if err != nil {
		log.Printf("Failed to create STT adapter: %v", err)
		return err
	}

	// Create audio handler to coordinate STT and event publishing
	handler := audio.NewHandler(adapter, s.publisher, interactionId, tenantId, segmentId)

	// Start the STT streaming session
	if err := handler.Start(ctx); err != nil {
		log.Printf("Failed to start STT session: %v", err)
		return err
	}
	defer handler.Close()

	// Start background goroutine to receive STT responses
	if ga, ok := adapter.(*google.Adapter); ok {
		go ga.Listen()
	}

	// Send first frame's audio if present
	if len(frame.Audio) > 0 {
		if err := handler.SendAudio(ctx, frame.Audio, frame.AudioOffsetMs); err != nil {
			log.Printf("Failed to send audio: %v", err)
			return err
		}
	}

	// Stream remaining audio frames until EOF or EndOfUtterance
	for {
		frame, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Stream recv error: %v", err)
			return err
		}

		if len(frame.Audio) > 0 {
			if err := handler.SendAudio(ctx, frame.Audio, frame.AudioOffsetMs); err != nil {
				log.Printf("Failed to send audio: %v", err)
				return err
			}
		}

		if frame.EndOfUtterance {
			break
		}
	}

	log.Printf("Stream completed: interactionId=%s segmentId=%s", interactionId, segmentId)

	return stream.SendAndClose(&pb.StreamAck{InteractionId: interactionId})
}

// createSTTAdapter creates an STT adapter instance based on configuration.
func (s *Server) createSTTAdapter(ctx context.Context) (stt.Adapter, error) {
	switch s.sttProvider {
	case "google":
		return google.New(ctx)
	case "mock":
		return mock.New(), nil
	default:
		log.Printf("Unknown STT provider '%s', using mock", s.sttProvider)
		return mock.New(), nil
	}
}
