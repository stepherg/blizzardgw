package config

import (
	"time"
)

// Config holds runtime configuration for the gateway.
type Config struct {
	Listen        string        `json:"listen"`
	ReadTimeout   time.Duration `json:"read_timeout"`
	WriteTimeout  time.Duration `json:"write_timeout"`
	IdleTimeout   time.Duration `json:"idle_timeout"`
	AllowedOrigin string        `json:"allowed_origin"`

	// Optional upstream WRP/Scytale endpoint for future forwarding.
	ScytaleURL  string `json:"scytale_url"`
	ScytaleAuth string `json:"scytale_auth"`
}

func Default() Config {
	return Config{
		Listen:       ":8082",
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
		// Defaults added: local Scytale test endpoint & basic auth token (base64 of user:pass)
		ScytaleURL:  "http://localhost:6300/api/v2/device", // assumed http scheme for provided host:port
		ScytaleAuth: "dXNlcjpwYXNz",
	}
}
