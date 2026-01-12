package google

import (
	"testing"

	speechpb "cloud.google.com/go/speech/apiv1/speechpb"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.LanguageCode != "en-US" {
		t.Errorf("expected default language 'en-US', got %s", cfg.LanguageCode)
	}
	if cfg.SampleRateHz != 8000 {
		t.Errorf("expected default sample rate 8000, got %d", cfg.SampleRateHz)
	}
	if cfg.InterimResults != true {
		t.Errorf("expected default interim results true, got %v", cfg.InterimResults)
	}
	if cfg.AudioEncoding != "LINEAR16" {
		t.Errorf("expected default encoding 'LINEAR16', got %s", cfg.AudioEncoding)
	}
}

func TestParseAudioEncoding(t *testing.T) {
	tests := []struct {
		input    string
		expected speechpb.RecognitionConfig_AudioEncoding
	}{
		{"LINEAR16", speechpb.RecognitionConfig_LINEAR16},
		{"MULAW", speechpb.RecognitionConfig_MULAW},
		{"FLAC", speechpb.RecognitionConfig_FLAC},
		{"AMR", speechpb.RecognitionConfig_AMR},
		{"AMR_WB", speechpb.RecognitionConfig_AMR_WB},
		{"OGG_OPUS", speechpb.RecognitionConfig_OGG_OPUS},
		{"SPEEX_WITH_HEADER_BYTE", speechpb.RecognitionConfig_SPEEX_WITH_HEADER_BYTE},
		{"WEBM_OPUS", speechpb.RecognitionConfig_WEBM_OPUS},
		{"UNKNOWN", speechpb.RecognitionConfig_LINEAR16}, // fallback
		{"invalid", speechpb.RecognitionConfig_LINEAR16}, // fallback
		{"", speechpb.RecognitionConfig_LINEAR16},        // fallback
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseAudioEncoding(tt.input)
			if got != tt.expected {
				t.Errorf("parseAudioEncoding(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestConfig_CustomValues(t *testing.T) {
	cfg := Config{
		LanguageCode:   "es-ES",
		SampleRateHz:   16000,
		InterimResults: false,
		AudioEncoding:  "MULAW",
	}

	if cfg.LanguageCode != "es-ES" {
		t.Errorf("expected language 'es-ES', got %s", cfg.LanguageCode)
	}
	if cfg.SampleRateHz != 16000 {
		t.Errorf("expected sample rate 16000, got %d", cfg.SampleRateHz)
	}
	if cfg.InterimResults != false {
		t.Errorf("expected interim results false, got %v", cfg.InterimResults)
	}
	if cfg.AudioEncoding != "MULAW" {
		t.Errorf("expected encoding 'MULAW', got %s", cfg.AudioEncoding)
	}
}

func TestParseAudioEncoding_CaseSensitive(t *testing.T) {
	// Encoding strings should be uppercase
	tests := []struct {
		input    string
		expected speechpb.RecognitionConfig_AudioEncoding
	}{
		{"linear16", speechpb.RecognitionConfig_LINEAR16}, // lowercase -> fallback
		{"Linear16", speechpb.RecognitionConfig_LINEAR16}, // mixed case -> fallback
		{"LINEAR16", speechpb.RecognitionConfig_LINEAR16}, // uppercase -> match
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseAudioEncoding(tt.input)
			if got != tt.expected {
				t.Errorf("parseAudioEncoding(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
