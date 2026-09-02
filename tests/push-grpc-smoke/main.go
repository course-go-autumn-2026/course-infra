// Command push-grpc-smoke exercises the public Push Service gRPC contract.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"

	pushv1 "github.com/course-go-autumn-2026/tripgo-infra/internal/gen/push/v1"
)

func main() {
	endpoint := flag.String("endpoint", "localhost:19095", "Push Service gRPC endpoint")
	expected := flag.String("expect", "success", "success, unavailable, resource-exhausted, or deadline")
	timeout := flag.Duration("timeout", 3*time.Second, "RPC timeout")
	flag.Parse()
	if err := run(*endpoint, *expected, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(endpoint, expected string, timeout time.Duration) error {
	connection, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if response, err := healthv1.NewHealthClient(connection).Check(ctx, &healthv1.HealthCheckRequest{Service: pushv1.PushService_ServiceDesc.ServiceName}); err != nil || response.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		return fmt.Errorf("gRPC health: response=%v error=%w", response, err)
	}
	reflectionClient, err := reflectionv1.NewServerReflectionClient(connection).ServerReflectionInfo(ctx)
	if err != nil {
		return fmt.Errorf("gRPC reflection: %w", err)
	}
	if err := reflectionClient.Send(&reflectionv1.ServerReflectionRequest{MessageRequest: &reflectionv1.ServerReflectionRequest_ListServices{ListServices: ""}}); err != nil {
		return fmt.Errorf("gRPC reflection send: %w", err)
	}
	if response, err := reflectionClient.Recv(); err != nil || len(response.GetListServicesResponse().GetService()) == 0 {
		return fmt.Errorf("gRPC reflection response=%v error=%w", response, err)
	}
	_, callErr := pushv1.NewPushServiceClient(connection).SendPush(ctx, &pushv1.SendPushRequest{
		RequestId: "grpc-smoke", RecipientId: "8860b315-ec86-42eb-a17c-7c163d721ff5",
		Kind: pushv1.PushKind_PUSH_KIND_REQUEST_POSITION,
		Data: map[string]string{"trip_id": "1f0a9c62-4a1c-4f2e-9d33-2a4bb0f0b111"},
	})
	expectedCode := map[string]codes.Code{
		"success": codes.OK, "unavailable": codes.Unavailable,
		"resource-exhausted": codes.ResourceExhausted, "deadline": codes.DeadlineExceeded,
	}[expected]
	if status.Code(callErr) != expectedCode {
		return fmt.Errorf("SendPush code=%s, want %s: %w", status.Code(callErr), expectedCode, callErr)
	}
	return nil
}
