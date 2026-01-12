package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear relevant env vars
	envVars := []string{
		"SERVICE_PRINCIPAL", "GRPC_PORT", "LOG_LEVEL",
		"STT_PROVIDER", "STT_LANGUAGE_CODE", "STT_SAMPLE_RATE_HZ",
		"STT_INTERIM_RESULTS", "STT_AUDIO_ENCODING",
		"SEGMENT_MAX_AUDIO_BYTES", "SEGMENT_MAX_DURATION", "SEGMENT_MAX_PARTIALS",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}

	cfg := Load()

	// Service defaults
	if cfg.Service.Principal != "svc-speech-ingress" {
		t.Errorf("expected default principal 'svc-speech-ingress', got %s", cfg.Service.Principal)
	}
	if cfg.Service.GRPCPort != "50051" {
		t.Errorf("expected default port '50051', got %s", cfg.Service.GRPCPort)
	}

	// STT defaults
	if cfg.STT.Provider != "mock" {
		t.Errorf("expected default STT provider 'mock', got %s", cfg.STT.Provider)
	}
	if cfg.STT.LanguageCode != "en-US" {
		t.Errorf("expected default language 'en-US', got %s", cfg.STT.LanguageCode)
	}
	if cfg.STT.SampleRateHz != 8000 {
		t.Errorf("expected default sample rate 8000, got %d", cfg.STT.SampleRateHz)
	}
	if cfg.STT.InterimResults != true {
		t.Errorf("expected default interim results true, got %v", cfg.STT.InterimResults)
	}
	if cfg.STT.AudioEncoding != "LINEAR16" {
		t.Errorf("expected default encoding 'LINEAR16', got %s", cfg.STT.AudioEncoding)
	}

	// Segment limits defaults
	if cfg.SegmentLimits.MaxAudioBytes != 5*1024*1024 {
		t.Errorf("expected default max audio bytes 5MB, got %d", cfg.SegmentLimits.MaxAudioBytes)
	}
	if cfg.SegmentLimits.MaxDuration != 5*time.Minute {
		t.Errorf("expected default max duration 5m, got %v", cfg.SegmentLimits.MaxDuration)
	}
	if cfg.SegmentLimits.MaxPartials != 500 {
		t.Errorf("expected default max partials 500, got %d", cfg.SegmentLimits.MaxPartials)
	}

	// Observability defaults
	if cfg.Observability.LogLevel != "info" {
		t.Errorf("expected default log level 'info', got %s", cfg.Observability.LogLevel)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	// Set custom env vars
	os.Setenv("SERVICE_PRINCIPAL", "custom-principal")
	os.Setenv("GRPC_PORT", "9999")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("STT_PROVIDER", "google")
	os.Setenv("STT_LANGUAGE_CODE", "es-ES")
	os.Setenv("STT_SAMPLE_RATE_HZ", "16000")
	os.Setenv("STT_INTERIM_RESULTS", "false")
	os.Setenv("STT_AUDIO_ENCODING", "MULAW")
	os.Setenv("SEGMENT_MAX_AUDIO_BYTES", "10485760")
	os.Setenv("SEGMENT_MAX_DURATION", "10m")
	os.Setenv("SEGMENT_MAX_PARTIALS", "1000")

	defer func() {
		// Clean up
		os.Unsetenv("SERVICE_PRINCIPAL")
		os.Unsetenv("GRPC_PORT")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("STT_PROVIDER")
		os.Unsetenv("STT_LANGUAGE_CODE")
		os.Unsetenv("STT_SAMPLE_RATE_HZ")
		os.Unsetenv("STT_INTERIM_RESULTS")
		os.Unsetenv("STT_AUDIO_ENCODING")
		os.Unsetenv("SEGMENT_MAX_AUDIO_BYTES")
		os.Unsetenv("SEGMENT_MAX_DURATION")
		os.Unsetenv("SEGMENT_MAX_PARTIALS")
	}()

	cfg := Load()

	if cfg.Service.Principal != "custom-principal" {
		t.Errorf("expected principal 'custom-principal', got %s", cfg.Service.Principal)
	}
	if cfg.Service.GRPCPort != "9999" {
		t.Errorf("expected port '9999', got %s", cfg.Service.GRPCPort)
	}
	if cfg.STT.Provider != "google" {
		t.Errorf("expected STT provider 'google', got %s", cfg.STT.Provider)
	}
	if cfg.STT.LanguageCode != "es-ES" {
		t.Errorf("expected language 'es-ES', got %s", cfg.STT.LanguageCode)
	}
	if cfg.STT.SampleRateHz != 16000 {
		t.Errorf("expected sample rate 16000, got %d", cfg.STT.SampleRateHz)
	}
	if cfg.STT.InterimResults != false {
		t.Errorf("expected interim results false, got %v", cfg.STT.InterimResults)
	}
	if cfg.STT.AudioEncoding != "MULAW" {
		t.Errorf("expected encoding 'MULAW', got %s", cfg.STT.AudioEncoding)
	}
	if cfg.SegmentLimits.MaxAudioBytes != 10485760 {
		t.Errorf("expected max audio bytes 10485760, got %d", cfg.SegmentLimits.MaxAudioBytes)
	}
	if cfg.SegmentLimits.MaxDuration != 10*time.Minute {
		t.Errorf("expected max duration 10m, got %v", cfg.SegmentLimits.MaxDuration)
	}
	if cfg.SegmentLimits.MaxPartials != 1000 {
		t.Errorf("expected max partials 1000, got %d", cfg.SegmentLimits.MaxPartials)
	}
	if cfg.Observability.LogLevel != "debug" {
		t.Errorf("expected log level 'debug', got %s", cfg.Observability.LogLevel)
	}
}

func TestLoad_InvalidValues_FallbackToDefaults(t *testing.T) {
	// Set invalid env vars
	os.Setenv("STT_SAMPLE_RATE_HZ", "not-a-number")
	os.Setenv("STT_INTERIM_RESULTS", "invalid")
	os.Setenv("SEGMENT_MAX_AUDIO_BYTES", "invalid")
	os.Setenv("SEGMENT_MAX_DURATION", "invalid")
	os.Setenv("SEGMENT_MAX_PARTIALS", "invalid")

	defer func() {
		os.Unsetenv("STT_SAMPLE_RATE_HZ")
		os.Unsetenv("STT_INTERIM_RESULTS")
		os.Unsetenv("SEGMENT_MAX_AUDIO_BYTES")
		os.Unsetenv("SEGMENT_MAX_DURATION")
		os.Unsetenv("SEGMENT_MAX_PARTIALS")
	}()

	cfg := Load()

	// Should fall back to defaults on parse errors
	if cfg.STT.SampleRateHz != 8000 {
		t.Errorf("expected default sample rate on invalid input, got %d", cfg.STT.SampleRateHz)
	}
	if cfg.STT.InterimResults != true {
		t.Errorf("expected default interim results on invalid input, got %v", cfg.STT.InterimResults)
	}
	if cfg.SegmentLimits.MaxAudioBytes != 5*1024*1024 {
		t.Errorf("expected default max audio bytes on invalid input, got %d", cfg.SegmentLimits.MaxAudioBytes)
	}
	if cfg.SegmentLimits.MaxDuration != 5*time.Minute {
		t.Errorf("expected default max duration on invalid input, got %v", cfg.SegmentLimits.MaxDuration)
	}
	if cfg.SegmentLimits.MaxPartials != 500 {
		t.Errorf("expected default max partials on invalid input, got %d", cfg.SegmentLimits.MaxPartials)
	}
}

func TestLoad_KafkaPrincipal_FallsBackToServicePrincipal(t *testing.T) {
	os.Setenv("SERVICE_PRINCIPAL", "my-service")
	os.Unsetenv("KAFKA_PRINCIPAL")

	defer os.Unsetenv("SERVICE_PRINCIPAL")

	cfg := Load()

	if cfg.Kafka.Principal != "my-service" {
		t.Errorf("expected Kafka principal to fall back to service principal, got %s", cfg.Kafka.Principal)
	}
}

func TestEnvOrDefaultBool(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		def      bool
		expected bool
	}{
		{"true string", "true", false, true},
		{"false string", "false", true, false},
		{"1", "1", false, true},
		{"0", "0", true, false},
		{"TRUE uppercase", "TRUE", false, true},
		{"invalid", "invalid", true, true},
		{"empty", "", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_BOOL_VAR"
			if tt.envValue != "" {
				os.Setenv(key, tt.envValue)
			} else {
				os.Unsetenv(key)
			}
			defer os.Unsetenv(key)

			got := envOrDefaultBool(key, tt.def)
			if got != tt.expected {
				t.Errorf("envOrDefaultBool(%s, %v) = %v, want %v", tt.envValue, tt.def, got, tt.expected)
			}
		})
	}
}
