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
	Service       ServiceConfig
	STT           STTConfig
	Kafka         KafkaConfig
	SegmentLimits SegmentLimitsConfig
	Observability ObservabilityConfig
}

// ServiceConfig holds service identity and runtime configuration.
type ServiceConfig struct {
	// Principal is the service identity for authentication/authorization.
	// Default: svc-speech-ingress
	Principal string

	// GRPCPort is the port for the gRPC server.
	// Default: 50051
	GRPCPort string
}

// STTConfig holds Speech-to-Text provider configuration.
// These parameters are passed to the STT provider (Google, etc.).
type STTConfig struct {
	// Provider is the STT provider to use ("google" or "mock").
	// Default: mock (for local dev safety)
	Provider string

	// LanguageCode is the BCP-47 language code for transcription.
	// Default: en-US
	LanguageCode string

	// SampleRateHz is the audio sample rate in Hertz.
	// Default: 8000 (telephony standard)
	SampleRateHz int

	// InterimResults enables partial/interim transcripts.
	// Default: true
	InterimResults bool

	// AudioEncoding is the audio encoding format.
	// Supported: LINEAR16, MULAW, FLAC, etc.
	// Default: LINEAR16
	AudioEncoding string
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

// Default STT parameters
const (
	DefaultLanguageCode   = "en-US"
	DefaultSampleRateHz   = 8000
	DefaultInterimResults = true
	DefaultAudioEncoding  = "LINEAR16"
)

// Default service identity
const (
	DefaultServicePrincipal = "svc-speech-ingress"
	DefaultGRPCPort         = "50051"
)

// Load reads configuration from environment variables.
func Load() *Config {
	servicePrincipal := envOrDefault("SERVICE_PRINCIPAL", DefaultServicePrincipal)

	return &Config{
		Service: ServiceConfig{
			Principal: servicePrincipal,
			GRPCPort:  envOrDefault("GRPC_PORT", DefaultGRPCPort),
		},
		STT: STTConfig{
			Provider:       envOrDefault("STT_PROVIDER", "mock"), // default to mock for local dev
			LanguageCode:   envOrDefault("STT_LANGUAGE_CODE", DefaultLanguageCode),
			SampleRateHz:   envOrDefaultInt("STT_SAMPLE_RATE_HZ", DefaultSampleRateHz),
			InterimResults: envOrDefaultBool("STT_INTERIM_RESULTS", DefaultInterimResults),
			AudioEncoding:  envOrDefault("STT_AUDIO_ENCODING", DefaultAudioEncoding),
		},
		Kafka: KafkaConfig{
			Enabled:      envOrDefault("KAFKA_ENABLED", "false") == "true",
			Brokers:      strings.Split(envOrDefault("KAFKA_BROKERS", "localhost:9092"), ","),
			TopicPartial: envOrDefault("KAFKA_TOPIC_PARTIAL", "interaction.transcript.partial"),
			TopicFinal:   envOrDefault("KAFKA_TOPIC_FINAL", "interaction.transcript.final"),
			Principal:    envOrDefault("KAFKA_PRINCIPAL", servicePrincipal), // fallback to service principal
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

func envOrDefaultBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
