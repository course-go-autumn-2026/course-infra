package main

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	pushv1 "github.com/course-go-autumn-2026/tripgo-infra/internal/gen/push/v1"
)

func TestRunSeparatesSetupAndPushDeadlines(t *testing.T) {
	const timeout = 100 * time.Millisecond
	pushCalled := make(chan struct{}, 1)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		switch info.FullMethod {
		case healthv1.Health_Check_FullMethodName:
			// Setup takes longer than the intentional SendPush deadline.
			select {
			case <-time.After(2 * timeout):
			case <-ctx.Done():
				return nil, status.FromContextError(ctx.Err()).Err()
			}
		case pushv1.PushService_SendPush_FullMethodName:
			pushCalled <- struct{}{}
			select {
			case <-time.After(2 * timeout):
				return &pushv1.SendPushResponse{}, nil
			case <-ctx.Done():
				return nil, status.FromContextError(ctx.Err()).Err()
			}
		}
		return handler(ctx, req)
	}))
	healthServer := health.NewServer()
	healthServer.SetServingStatus(pushv1.PushService_ServiceDesc.ServiceName, healthv1.HealthCheckResponse_SERVING)
	healthv1.RegisterHealthServer(server, healthServer)
	pushv1.RegisterPushServiceServer(server, &pushv1.UnimplementedPushServiceServer{})
	reflection.Register(server)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	go func() { _ = server.Serve(listener) }()

	if err := run(listener.Addr().String(), "deadline", timeout); err != nil {
		t.Fatal(err)
	}
	select {
	case <-pushCalled:
	default:
		t.Fatal("deadline expired before SendPush reached the server")
	}
}
