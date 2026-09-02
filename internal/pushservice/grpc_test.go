package pushservice

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pushv1 "github.com/course-go-autumn-2026/tripgo-infra/internal/gen/push/v1"
)

func TestGRPCPushHealthReflectionAndCodes(t *testing.T) {
	config := testConfig()
	service := NewService(config, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	server, _ := NewGRPCServer(service)
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	healthResponse, err := healthv1.NewHealthClient(connection).Check(t.Context(), &healthv1.HealthCheckRequest{Service: pushv1.PushService_ServiceDesc.ServiceName})
	if err != nil || healthResponse.GetStatus() != healthv1.HealthCheckResponse_SERVING {
		t.Fatalf("health = %v, %v", healthResponse, err)
	}
	reflectionClient, err := reflectionv1.NewServerReflectionClient(connection).ServerReflectionInfo(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := reflectionClient.Send(&reflectionv1.ServerReflectionRequest{MessageRequest: &reflectionv1.ServerReflectionRequest_ListServices{ListServices: ""}}); err != nil {
		t.Fatal(err)
	}
	reflectionResponse, err := reflectionClient.Recv()
	if err != nil || len(reflectionResponse.GetListServicesResponse().GetService()) == 0 {
		t.Fatalf("reflection = %v, %v", reflectionResponse, err)
	}

	client := pushv1.NewPushServiceClient(connection)
	valid := &pushv1.SendPushRequest{
		RequestId: "request-1", RecipientId: "8860b315-ec86-42eb-a17c-7c163d721ff5",
		Kind: pushv1.PushKind_PUSH_KIND_REQUEST_POSITION,
		Data: map[string]string{"trip_id": "1f0a9c62-4a1c-4f2e-9d33-2a4bb0f0b111"},
	}
	if _, err := client.SendPush(t.Context(), valid); err != nil {
		t.Fatal(err)
	}
	invalid := &pushv1.SendPushRequest{
		RequestId: valid.GetRequestId(), RecipientId: "invalid", Kind: valid.GetKind(), Data: valid.GetData(),
	}
	if _, err := client.SendPush(t.Context(), invalid); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid code = %s, error = %v", status.Code(err), err)
	}

	rateLimit := 1
	if err := service.PatchBehaviour(BehaviourPatch{RateLimitRPS: &rateLimit}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SendPush(t.Context(), valid); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SendPush(t.Context(), valid); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("rate limit code = %s, error = %v", status.Code(err), err)
	}

	failRate := 1.0
	rateLimit = 0
	if err := service.PatchBehaviour(BehaviourPatch{FailRate: &failRate, RateLimitRPS: &rateLimit}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SendPush(t.Context(), valid); status.Code(err) != codes.Unavailable {
		t.Fatalf("failure code = %s, error = %v", status.Code(err), err)
	}
	failRate = 0
	latency := int64(1000)
	if err := service.PatchBehaviour(BehaviourPatch{FailRate: &failRate, LatencyMS: &latency}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := client.SendPush(ctx, valid); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("deadline code = %s, error = %v", status.Code(err), err)
	}
}
