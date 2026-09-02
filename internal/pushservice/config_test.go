package pushservice

import (
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:18090")
	t.Setenv("GRPC_ADDR", "127.0.0.1:19095")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("PUSH_FAIL_RATE", "0.5")
	t.Setenv("PUSH_LATENCY_MS", "25")
	t.Setenv("PUSH_RATE_LIMIT_RPS", "0")
	t.Setenv("PUSH_RATE_LIMIT_BURST", "7")
	t.Setenv("SHUTDOWN_TIMEOUT", "3s")
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.HTTPAddr != "127.0.0.1:18090" || config.GRPCAddr != "127.0.0.1:19095" || config.LogLevel != "debug" || config.FailRate != 0.5 || config.Latency != 25*time.Millisecond || config.RateLimitRPS != 0 || config.RateLimitBurst != 7 || config.ShutdownTimeout != 3*time.Second {
		t.Fatalf("config = %+v", config)
	}
}

func TestConfigRejectsInvalidValues(t *testing.T) {
	t.Setenv("PUSH_FAIL_RATE", "1.1")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("invalid fail rate accepted")
	}
}
