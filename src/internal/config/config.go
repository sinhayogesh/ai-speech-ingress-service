// Package config provides configuration loading from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all service configuration.
type Config struct {
	Port          string
	STTProvider   string // "google" or "mock"
	Kafka         KafkaConfig
	SegmentLimits SegmentLimitsConfig
	Observability ObservabilityConfig
}

// SegmentLimitsConfig holds safety limits for segment processing.
// These are guardrails to prevent unbounded resource usage.
type SegmentLimitsConfig struct {
	// MaxAudioBytes is the maximum buffered audio bytes per segment.
	// If exceeded, the segment is dropped. Default: 5MB (625 seconds at 8kHz 16-bit mono)
	MaxAudioBytes int64

	// MaxDuration is the maximum duration of a single segment.
	// If exceeded, the segment is dropped. Default: 5 minutes
	MaxDuration time.Duration

	// MaxPartials is the maximum number of partial transcripts per segment.
	// If exceeded, the segment is dropped. Default: 500
	MaxPartials int
}

// KafkaConfig holds Kafka publisher configuration.
type KafkaConfig struct {
	Enabled      bool
	Brokers      []string
	TopicPartial string // Topic for partial transcripts
	TopicFinal   string // Topic for final transcripts
	Principal    string
}

// ObservabilityConfig holds observability settings.
type ObservabilityConfig struct {
	// MetricsPort is the port for the Prometheus metrics HTTP server.
	MetricsPort string

	// MetricsEnabled enables/disables the metrics server.
	MetricsEnabled bool

	// LogLevel is the zerolog log level (debug, info, warn, error).
	LogLevel string

	// LogFormat is the log output format (json, console).
	LogFormat string
}

// Default segment limits - safety guardrails
const (
	DefaultMaxAudioBytes = 5 * 1024 * 1024 // 5MB (~625 seconds at 8kHz 16-bit mono)
	DefaultMaxDuration   = 5 * time.Minute // 5 minutes max segment
	DefaultMaxPartials   = 500             // 500 partials max per segment
)

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		Port:        envOrDefault("GRPC_PORT", "50051"),
		STTProvider: envOrDefault("STT_PROVIDER", "mock"), // default to mock for local dev
		Kafka: KafkaConfig{
			Enabled:      envOrDefault("KAFKA_ENABLED", "false") == "true",
			Brokers:      strings.Split(envOrDefault("KAFKA_BROKERS", "localhost:9092"), ","),
			TopicPartial: envOrDefault("KAFKA_TOPIC_PARTIAL", "interaction.transcript.partial"),
			TopicFinal:   envOrDefault("KAFKA_TOPIC_FINAL", "interaction.transcript.final"),
			Principal:    envOrDefault("KAFKA_PRINCIPAL", "svc-speech-ingress"),
		},
		SegmentLimits: SegmentLimitsConfig{
			MaxAudioBytes: envOrDefaultInt64("SEGMENT_MAX_AUDIO_BYTES", DefaultMaxAudioBytes),
			MaxDuration:   envOrDefaultDuration("SEGMENT_MAX_DURATION", DefaultMaxDuration),
			MaxPartials:   envOrDefaultInt("SEGMENT_MAX_PARTIALS", DefaultMaxPartials),
		},
		Observability: ObservabilityConfig{
			MetricsPort:    envOrDefault("METRICS_PORT", "9090"),
			MetricsEnabled: envOrDefault("METRICS_ENABLED", "true") == "true",
			LogLevel:       envOrDefault("LOG_LEVEL", "info"),
			LogFormat:      envOrDefault("LOG_FORMAT", "json"),
		},
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return def
}

func envOrDefaultInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func envOrDefaultDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
