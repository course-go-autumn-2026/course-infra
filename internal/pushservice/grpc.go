package pushservice

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pushv1 "github.com/course-go-autumn-2026/tripgo-infra/internal/gen/push/v1"
)

type grpcPushServer struct {
	pushv1.UnimplementedPushServiceServer
	service *Service
}

// NewGRPCServer constructs Push, standard health, and reflection services.
func NewGRPCServer(service *Service) (*grpc.Server, *health.Server) {
	server := grpc.NewServer()
	pushv1.RegisterPushServiceServer(server, &grpcPushServer{service: service})
	healthServer := health.NewServer()
	healthv1.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(pushv1.PushService_ServiceDesc.ServiceName, healthv1.HealthCheckResponse_SERVING)
	reflection.Register(server)
	return server, healthServer
}

func (s *grpcPushServer) SendPush(ctx context.Context, request *pushv1.SendPushRequest) (*pushv1.SendPushResponse, error) {
	push := PushRequest{
		RequestID: request.GetRequestId(), RecipientID: request.GetRecipientId(),
		Kind: grpcKind(request.GetKind()), Title: request.GetTitle(), Body: request.GetBody(), Data: request.GetData(),
	}
	accepted, failure := s.service.Process(ctx, "grpc", push)
	if failure != nil {
		switch failure.Kind {
		case FailureInvalid:
			return nil, status.Error(codes.InvalidArgument, failure.Error())
		case FailureRateLimited:
			return nil, status.Error(codes.ResourceExhausted, failure.Error())
		case FailureInjected:
			return nil, status.Error(codes.Unavailable, failure.Error())
		case FailureCanceled:
			if errors.Is(failure.Err, context.DeadlineExceeded) {
				return nil, status.Error(codes.DeadlineExceeded, failure.Error())
			}
			return nil, status.Error(codes.Canceled, failure.Error())
		default:
			return nil, status.Error(codes.Unavailable, failure.Error())
		}
	}
	return &pushv1.SendPushResponse{MessageId: accepted.MessageID, AcceptedAt: timestamppb.New(accepted.AcceptedAt)}, nil
}

func grpcKind(kind pushv1.PushKind) string {
	switch kind {
	case pushv1.PushKind_PUSH_KIND_REQUEST_POSITION:
		return "REQUEST_POSITION"
	case pushv1.PushKind_PUSH_KIND_TRIP_CREATED:
		return "TRIP_CREATED"
	case pushv1.PushKind_PUSH_KIND_TRIP_COMPLETED:
		return "TRIP_COMPLETED"
	default:
		return ""
	}
}
