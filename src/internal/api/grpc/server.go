// Package grpcapi provides the gRPC server implementation for the AI Speech Ingress service.
package grpcapi

import (
	"context"
	"errors"
	"io"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	segments      *segment.Generator
	publisher     *events.Publisher
	validator     *schema.Validator
	sttProvider   string
	segmentLimits audio.SegmentLimits
}

// Register creates a new Server and registers it with the gRPC server.
func Register(g *grpc.Server, publisher *events.Publisher, sttProvider string) {
	RegisterWithLimits(g, publisher, sttProvider, audio.DefaultLimits())
}

// RegisterWithLimits creates a new Server with custom segment limits.
func RegisterWithLimits(g *grpc.Server, publisher *events.Publisher, sttProvider string, limits audio.SegmentLimits) {
	s := &Server{
		segments:      segment.New(),
		publisher:     publisher,
		validator:     schema.New(),
		sttProvider:   sttProvider,
		segmentLimits: limits,
	}
	log.Printf("Using STT provider: %s, limits: maxAudioBytes=%d maxDuration=%v maxPartials=%d",
		sttProvider, limits.MaxAudioBytes, limits.MaxDuration, limits.MaxPartials)
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
	// Pass segment generator so handler can create new segments on utterance boundaries
	// Use configured limits for backpressure safety
	handler := audio.NewHandlerWithLimits(adapter, s.publisher, s.segments, interactionId, tenantId, segmentId, s.segmentLimits)

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
			// Normal end of stream
			break
		}
		if err != nil {
			// Handle client disconnect or stream errors
			// "Silence > bad data" - drop the segment, emit no final
			handler.DropSegment(classifyStreamError(err))
			log.Printf("Stream error (segment dropped): interactionId=%s segmentId=%s err=%v",
				interactionId, handler.GetSegmentId(), err)
			// Return nil to avoid double-logging; segment is already dropped
			return nil
		}

		// Check for context cancellation (client disconnect)
		if ctx.Err() != nil {
			handler.DropSegment("context cancelled: " + ctx.Err().Error())
			log.Printf("Context cancelled (segment dropped): interactionId=%s segmentId=%s err=%v",
				interactionId, handler.GetSegmentId(), ctx.Err())
			return nil
		}

		if len(frame.Audio) > 0 {
			if err := handler.SendAudio(ctx, frame.Audio, frame.AudioOffsetMs); err != nil {
				handler.DropSegment("send audio failed: " + err.Error())
				log.Printf("Failed to send audio (segment dropped): interactionId=%s segmentId=%s err=%v",
					interactionId, handler.GetSegmentId(), err)
				return nil
			}
		}

		if frame.EndOfUtterance {
			break
		}
	}

	// Log final state
	finalState := handler.GetSegmentState()
	if handler.IsSegmentDropped() {
		log.Printf("Stream ended with DROPPED segment: interactionId=%s segmentId=%s state=%s",
			interactionId, handler.GetSegmentId(), finalState)
	} else {
		log.Printf("Stream completed: interactionId=%s segmentId=%s state=%s utterances=%d",
			interactionId, handler.GetSegmentId(), finalState, handler.GetUtteranceCount())
	}

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

// classifyStreamError returns a human-readable reason for stream errors.
// Used for logging when dropping segments due to stream failures.
func classifyStreamError(err error) string {
	if err == nil {
		return "unknown"
	}

	// Check for context errors
	if errors.Is(err, context.Canceled) {
		return "client disconnect (context canceled)"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout (deadline exceeded)"
	}

	// Check for gRPC status codes
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Canceled:
			return "client disconnect (gRPC canceled)"
		case codes.DeadlineExceeded:
			return "timeout (gRPC deadline exceeded)"
		case codes.Unavailable:
			return "network error (unavailable)"
		case codes.ResourceExhausted:
			return "resource exhausted"
		case codes.Internal:
			return "internal error"
		default:
			return "gRPC error: " + st.Code().String()
		}
	}

	// Check for EOF (unexpected connection close)
	if errors.Is(err, io.EOF) || err.Error() == "EOF" {
		return "unexpected connection close (EOF)"
	}

	return "stream error: " + err.Error()
}
