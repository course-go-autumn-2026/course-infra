// Package pushservice owns the Push Service application.
package pushservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
)

// App owns HTTP and gRPC listener lifecycle.
type App struct {
	Config *Config
	Logger *slog.Logger
}

// Run starts both transports and gracefully stops them on context cancellation.
func (app App) Run(ctx context.Context) error {
	config, err := app.config()
	if err != nil {
		return err
	}
	logger := app.Logger
	if logger == nil {
		logger = newJSONLogger(config.LogLevel)
	}
	service := NewService(config, logger)
	httpListener, err := net.Listen("tcp", config.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen HTTP on %s: %w", config.HTTPAddr, err)
	}
	defer func() { _ = httpListener.Close() }()
	grpcListener, err := net.Listen("tcp", config.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen gRPC on %s: %w", config.GRPCAddr, err)
	}
	defer func() { _ = grpcListener.Close() }()

	httpServer := newHTTPServer(service)
	grpcServer, healthServer := NewGRPCServer(service)
	errorsChannel := make(chan error, 2)
	go func() {
		err := httpServer.Serve(httpListener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errorsChannel <- err
	}()
	go func() { errorsChannel <- grpcServer.Serve(grpcListener) }()
	logger.Info("Push Service started", "http_addr", httpListener.Addr().String(), "grpc_addr", grpcListener.Addr().String())

	var runError error
	select {
	case <-ctx.Done():
	case runError = <-errorsChannel:
		if runError != nil {
			runError = fmt.Errorf("serve Push Service: %w", runError)
		}
	}

	healthServer.Shutdown()
	shutdownContext, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil && runError == nil {
		runError = fmt.Errorf("shutdown HTTP server: %w", err)
	}
	grpcStopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcStopped)
	}()
	select {
	case <-grpcStopped:
	case <-shutdownContext.Done():
		grpcServer.Stop()
		if runError == nil {
			runError = fmt.Errorf("shutdown gRPC server: %w", shutdownContext.Err())
		}
	}
	logger.Info("Push Service stopped")
	return runError
}

func newHTTPServer(service *Service) *http.Server {
	return &http.Server{
		Handler:           HTTPHandler(service),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// Behaviour latency has a deliberately unbounded public maximum. A server
		// WriteTimeout would accept work and then silently discard the response.
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}
}

func (app App) config() (Config, error) {
	if app.Config == nil {
		return ConfigFromEnv()
	}
	if err := app.Config.Validate(); err != nil {
		return Config{}, err
	}
	return *app.Config, nil
}

func newJSONLogger(levelName string) *slog.Logger {
	level := slog.LevelInfo
	switch levelName {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
