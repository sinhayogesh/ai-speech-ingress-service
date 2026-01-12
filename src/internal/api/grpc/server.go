package grpcapi

import (
	"context"
	"time"

	"google.golang.org/grpc"

	"ai-speech-ingress-service/internal/events"
	"ai-speech-ingress-service/internal/models"
	"ai-speech-ingress-service/internal/schema"
	"ai-speech-ingress-service/internal/service/segment"
	"ai-speech-ingress-service/internal/service/transcription"
	pb "ai-speech-ingress-service/proto"
)

type Server struct {
	pb.UnimplementedAudioStreamServiceServer
	segments  *segment.Generator
	publisher *events.Publisher
	validator *schema.Validator
}

func Register(g *grpc.Server) {
	s := &Server{
		segments:  segment.New(),
		publisher: events.New("svc-speech-ingress"),
		validator: schema.New(),
	}
	pb.RegisterAudioStreamServiceServer(g, s)
}

func (s *Server) StreamAudio(stream pb.AudioStreamService_StreamAudioServer) error {
	ctx := context.Background()

	frame, err := stream.Recv()
	if err != nil {
		return err
	}

	interactionId := frame.InteractionId
	tenantId := frame.TenantId
	segmentId := s.segments.Next(interactionId)

	for _, text := range transcription.PartialTexts() {
		ev := models.TranscriptPartial{
			EventType:     "interaction.transcript.partial",
			InteractionID: interactionId,
			TenantID:      tenantId,
			SegmentID:     segmentId,
			Text:          text,
			Timestamp:     time.Now().UnixMilli(),
		}

		s.validator.Validate(ev)
		s.publisher.Publish(ctx, "interaction.transcript.partial", interactionId, ev)
		time.Sleep(200 * time.Millisecond)
	}

	finalText, confidence := transcription.FinalText()

	finalEv := models.TranscriptFinal{
		EventType:     "interaction.transcript.final",
		InteractionID: interactionId,
		TenantID:      tenantId,
		SegmentID:     segmentId,
		Text:          finalText,
		Confidence:    confidence,
		AudioOffsetMs: 18420,
		Timestamp:     time.Now().UnixMilli(),
	}

	s.validator.Validate(finalEv)
	s.publisher.Publish(ctx, "interaction.transcript.final", interactionId, finalEv)

	return stream.SendAndClose(&pb.StreamAck{InteractionId: interactionId})
}
