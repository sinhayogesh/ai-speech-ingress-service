package config

import (
	"os"
)

// Configuration holds process configuration loaded from environment variables.
type Configuration struct {
	// HTTPListenAddress is the main HTTP listen address, e.g. ":8080".
	HTTPListenAddress string

	// HealthListenAddress is the address for liveness/readiness checks, e.g. ":8081".
	HealthListenAddress string
}

// Load reads configuration from environment variables with sane defaults.
func Load() (*Configuration, error) {
	cfg := &Configuration{
		HTTPListenAddress:   envOrDefault("HTTP_LISTEN_ADDRESS", ":8080"),
		HealthListenAddress: envOrDefault("HEALTH_LISTEN_ADDRESS", ":8081"),
	}

	return cfg, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

