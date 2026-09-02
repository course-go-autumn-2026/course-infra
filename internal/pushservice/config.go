package pushservice

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr = ":8090"
	defaultGRPCAddr = ":9095"
)

// Config contains process-level Push Service settings.
type Config struct {
	HTTPAddr        string
	GRPCAddr        string
	LogLevel        string
	FailRate        float64
	Latency         time.Duration
	RateLimitRPS    int
	RateLimitBurst  int
	ShutdownTimeout time.Duration
}

// ConfigFromEnv loads and validates the Push Service environment contract.
func ConfigFromEnv() (Config, error) {
	config := Config{
		HTTPAddr: defaultHTTPAddr, GRPCAddr: defaultGRPCAddr, LogLevel: "info",
		RateLimitRPS: 20, RateLimitBurst: 40, ShutdownTimeout: 10 * time.Second,
	}
	config.HTTPAddr = envOr("HTTP_ADDR", config.HTTPAddr)
	config.GRPCAddr = envOr("GRPC_ADDR", config.GRPCAddr)
	config.LogLevel = strings.ToLower(envOr("LOG_LEVEL", config.LogLevel))
	var err error
	if config.FailRate, err = envFloat("PUSH_FAIL_RATE", 0); err != nil {
		return Config{}, err
	}
	latencyMS, err := envInt("PUSH_LATENCY_MS", 0)
	if err != nil {
		return Config{}, err
	}
	if latencyMS < 0 || latencyMS > int((time.Duration(1<<63-1))/time.Millisecond) {
		return Config{}, fmt.Errorf("PUSH_LATENCY_MS is outside the supported duration range")
	}
	config.Latency = time.Duration(latencyMS) * time.Millisecond
	if config.RateLimitRPS, err = envInt("PUSH_RATE_LIMIT_RPS", config.RateLimitRPS); err != nil {
		return Config{}, err
	}
	if config.RateLimitBurst, err = envInt("PUSH_RATE_LIMIT_BURST", config.RateLimitBurst); err != nil {
		return Config{}, err
	}
	if value, ok := os.LookupEnv("SHUTDOWN_TIMEOUT"); ok {
		config.ShutdownTimeout, err = time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT must be a duration: %w", err)
		}
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Validate checks process configuration before listeners are opened.
func (c Config) Validate() error {
	if strings.TrimSpace(c.HTTPAddr) == "" || strings.TrimSpace(c.GRPCAddr) == "" {
		return fmt.Errorf("HTTP_ADDR and GRPC_ADDR must be non-empty")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}
	if math.IsNaN(c.FailRate) || math.IsInf(c.FailRate, 0) || c.FailRate < 0 || c.FailRate > 1 {
		return fmt.Errorf("PUSH_FAIL_RATE must be between 0 and 1")
	}
	if c.Latency < 0 {
		return fmt.Errorf("PUSH_LATENCY_MS must be non-negative")
	}
	if c.RateLimitRPS < 0 {
		return fmt.Errorf("PUSH_RATE_LIMIT_RPS must be non-negative")
	}
	if c.RateLimitBurst <= 0 {
		return fmt.Errorf("PUSH_RATE_LIMIT_BURST must be positive")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}
	return nil
}

func envOr(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}

func envFloat(name string, fallback float64) (float64, error) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", name, err)
	}
	return parsed, nil
}

func envInt(name string, fallback int) (int, error) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}
