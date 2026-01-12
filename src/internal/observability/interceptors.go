// Package observability provides gRPC interceptors for metrics and logging.
package observability

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"ai-speech-ingress-service/internal/observability/metrics"
)

// UnaryServerInterceptor returns a gRPC unary interceptor for metrics and logging.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		st, _ := status.FromError(err)

		log.Info().
			Str("method", info.FullMethod).
			Str("code", st.Code().String()).
			Dur("duration", duration).
			Msg("gRPC unary call")

		return resp, err
	}
}

// StreamServerInterceptor returns a gRPC stream interceptor for metrics and logging.
func StreamServerInterceptor(m *metrics.Metrics) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()
		m.RecordStreamStart()

		err := handler(srv, ss)

		duration := time.Since(start)
		success := err == nil
		m.RecordStreamEnd(success, duration.Seconds())

		st, _ := status.FromError(err)

		log.Info().
			Str("method", info.FullMethod).
			Str("code", st.Code().String()).
			Dur("duration", duration).
			Bool("success", success).
			Msg("gRPC stream completed")

		return err
	}
}

