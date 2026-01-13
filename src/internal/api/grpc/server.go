// Package grpcapi provides the gRPC server implementation for the AI Speech Ingress service.
package grpcapi

import (
	"context"
	"errors"
	"io"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"ai-speech-ingress-service/internal/config"
	"ai-speech-ingress-service/internal/events"
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
	sttConfig     config.STTConfig
	segmentLimits audio.SegmentLimits
}

// Register creates a new Server and registers it with the gRPC server using defaults.
func Register(g *grpc.Server, publisher *events.Publisher, sttProvider string) {
	RegisterWithConfig(g, publisher, config.STTConfig{Provider: sttProvider}, audio.DefaultLimits())
}

// RegisterWithLimits creates a new Server with custom segment limits (legacy, use RegisterWithConfig).
func RegisterWithLimits(g *grpc.Server, publisher *events.Publisher, sttProvider string, limits audio.SegmentLimits) {
	RegisterWithConfig(g, publisher, config.STTConfig{Provider: sttProvider}, limits)
}

// RegisterWithConfig creates a new Server with full STT config and segment limits.
func RegisterWithConfig(g *grpc.Server, publisher *events.Publisher, sttCfg config.STTConfig, limits audio.SegmentLimits) {
	// Apply defaults for any unset STT config values
	if sttCfg.LanguageCode == "" {
		sttCfg.LanguageCode = config.DefaultLanguageCode
	}
	if sttCfg.SampleRateHz == 0 {
		sttCfg.SampleRateHz = config.DefaultSampleRateHz
	}
	if sttCfg.AudioEncoding == "" {
		sttCfg.AudioEncoding = config.DefaultAudioEncoding
	}

	s := &Server{
		segments:      segment.New(),
		publisher:     publisher,
		sttConfig:     sttCfg,
		segmentLimits: limits,
	}

	log.Info().
		Str("provider", sttCfg.Provider).
		Str("language", sttCfg.LanguageCode).
		Int("sampleRate", sttCfg.SampleRateHz).
		Bool("interim", sttCfg.InterimResults).
		Str("encoding", sttCfg.AudioEncoding).
		Msg("STT config initialized")

	log.Info().
		Int64("maxAudioBytes", limits.MaxAudioBytes).
		Dur("maxDuration", limits.MaxDuration).
		Int("maxPartials", limits.MaxPartials).
		Msg("Segment limits configured")

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

	log.Info().
		Str("interactionId", interactionId).
		Str("tenantId", tenantId).
		Str("segmentId", segmentId).
		Msg("Starting stream")

	// Create and initialize STT adapter
	adapter, err := s.createSTTAdapter(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create STT adapter")
		return err
	}

	// Create audio handler to coordinate STT and event publishing
	// Pass segment generator so handler can create new segments on utterance boundaries
	// Use configured limits for backpressure safety
	handler := audio.NewHandlerWithLimits(adapter, s.publisher, s.segments, interactionId, tenantId, segmentId, s.segmentLimits)

	// Enable continuous mode when SingleUtterance is disabled
	// In this mode, each final creates a new segment automatically
	if !s.sttConfig.SingleUtterance {
		handler.SetContinuousMode(true)
	}

	// Start the STT streaming session
	if err := handler.Start(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to start STT session")
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
			log.Error().Err(err).Msg("Failed to send audio")
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
			log.Warn().
				Err(err).
				Str("interactionId", interactionId).
				Str("segmentId", handler.GetSegmentId()).
				Msg("Stream error (segment dropped)")
			// Return nil to avoid double-logging; segment is already dropped
			return nil
		}

		// Check for context cancellation (client disconnect)
		if ctx.Err() != nil {
			handler.DropSegment("context cancelled: " + ctx.Err().Error())
			log.Warn().
				Err(ctx.Err()).
				Str("interactionId", interactionId).
				Str("segmentId", handler.GetSegmentId()).
				Msg("Context cancelled (segment dropped)")
			return nil
		}

		if len(frame.Audio) > 0 {
			if err := handler.SendAudio(ctx, frame.Audio, frame.AudioOffsetMs); err != nil {
				handler.DropSegment("send audio failed: " + err.Error())
				log.Warn().
					Err(err).
					Str("interactionId", interactionId).
					Str("segmentId", handler.GetSegmentId()).
					Msg("Failed to send audio (segment dropped)")
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
		log.Warn().
			Str("interactionId", interactionId).
			Str("segmentId", handler.GetSegmentId()).
			Str("state", finalState.String()).
			Msg("Stream ended with DROPPED segment")
	} else {
		log.Info().
			Str("interactionId", interactionId).
			Str("segmentId", handler.GetSegmentId()).
			Str("state", finalState.String()).
			Int("utterances", handler.GetUtteranceCount()).
			Msg("Stream completed")
	}

	return stream.SendAndClose(&pb.StreamAck{InteractionId: interactionId})
}

// createSTTAdapter creates an STT adapter instance based on configuration.
func (s *Server) createSTTAdapter(ctx context.Context) (stt.Adapter, error) {
	switch s.sttConfig.Provider {
	case "google":
		cfg := google.Config{
			LanguageCode:    s.sttConfig.LanguageCode,
			SampleRateHz:    s.sttConfig.SampleRateHz,
			InterimResults:  s.sttConfig.InterimResults,
			AudioEncoding:   s.sttConfig.AudioEncoding,
			SingleUtterance: s.sttConfig.SingleUtterance,
		}
		return google.NewWithConfig(ctx, cfg)
	case "mock":
		return mock.New(), nil
	default:
		log.Warn().
			Str("provider", s.sttConfig.Provider).
			Msg("Unknown STT provider, using mock")
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
