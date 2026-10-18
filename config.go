package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Build metadata, injected at build time with -ldflags -X.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

// Defaults match the original binary so existing users see no change.
type Config struct {
	Port           string
	MetricsPort    string
	MaxMemoryBytes int64
	AuthToken      string
	LogLevel       slog.Level
	// Off by default: span creation adds allocations to the hot path.
	OTelEnabled    bool
	OTelEndpoint   string
	OTelSampleRate float64
	ShutdownDelay  time.Duration
	FailReadiness  bool
	// The downward API can expose the container limit but cannot scale it,
	// so the 90% soft limit for the Go runtime is derived here.
	SoftMemoryLimit int64
}

func loadConfig() (Config, error) {
	cfg := Config{
		Port:           envOr("PORT", "7171"),
		MetricsPort:    envOr("METRICS_PORT", "9091"),
		MaxMemoryBytes: MaxMemoryBytes,
		AuthToken:      os.Getenv("AUTH_TOKEN"),
		LogLevel:       slog.LevelInfo,
		OTelEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OTelSampleRate: 1.0,
		ShutdownDelay:  2 * time.Second,
	}

	if v := os.Getenv("MAX_MEMORY_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return cfg, fmt.Errorf("invalid MAX_MEMORY_BYTES %q", v)
		}
		cfg.MaxMemoryBytes = n
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(strings.ToUpper(v))); err != nil {
			return cfg, fmt.Errorf("invalid LOG_LEVEL %q", v)
		}
	}

	var err error
	if cfg.OTelEnabled, err = envBool("OTEL_ENABLED"); err != nil {
		return cfg, err
	}
	if cfg.FailReadiness, err = envBool("FAIL_READINESS"); err != nil {
		return cfg, err
	}

	if v := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 1 {
			return cfg, fmt.Errorf("invalid OTEL_TRACES_SAMPLER_ARG %q", v)
		}
		cfg.OTelSampleRate = f
	}

	if v := os.Getenv("SHUTDOWN_DELAY"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return cfg, fmt.Errorf("invalid SHUTDOWN_DELAY %q", v)
		}
		cfg.ShutdownDelay = d
	}

	if v := os.Getenv("MEMORY_LIMIT_BYTES"); v != "" && os.Getenv("GOMEMLIMIT") == "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return cfg, fmt.Errorf("invalid MEMORY_LIMIT_BYTES %q", v)
		}
		cfg.SoftMemoryLimit = n / 10 * 9
	}

	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q", key, v)
	}
	return b, nil
}
