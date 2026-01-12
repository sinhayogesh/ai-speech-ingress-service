package config

import "os"

type Config struct {
	Port string
}

func Load() *Config {
	return &Config{
		Port: envOrDefault("GRPC_PORT", "50051"),
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
