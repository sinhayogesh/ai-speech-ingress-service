// Package config provides configuration loading from environment variables.
package config

import (
	"os"
	"strings"
)

// Config holds all service configuration.
type Config struct {
	Port        string
	STTProvider string // "google" or "mock"
	Kafka       KafkaConfig
}

// KafkaConfig holds Kafka publisher configuration.
type KafkaConfig struct {
	Enabled   bool
	Brokers   []string
	Topic     string
	Principal string
}

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		Port:        envOrDefault("GRPC_PORT", "50051"),
		STTProvider: envOrDefault("STT_PROVIDER", "mock"), // default to mock for local dev
		Kafka: KafkaConfig{
			Enabled:   envOrDefault("KAFKA_ENABLED", "false") == "true",
			Brokers:   strings.Split(envOrDefault("KAFKA_BROKERS", "localhost:9092"), ","),
			Topic:     envOrDefault("KAFKA_TOPIC", "interaction.transcripts"),
			Principal: envOrDefault("KAFKA_PRINCIPAL", "svc-speech-ingress"),
		},
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
