// Command otlp-grpc-smoke sends one deterministic trace through an OTLP/gRPC endpoint.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"time"

	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	endpoint := flag.String("endpoint", "localhost:22317", "OTLP/gRPC endpoint")
	service := flag.String("service", "tripgo-stage6-grpc-smoke", "service.name resource attribute")
	spanName := flag.String("span", "stage6-grpc-trace", "span name")
	flag.Parse()

	if err := send(*endpoint, *service, *spanName); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func send(endpoint, service, spanName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("create OTLP/gRPC client: %w", err)
	}
	defer func() { _ = connection.Close() }()

	traceID, _ := hex.DecodeString("fedcba9876543210fedcba9876543210")
	spanID, _ := hex.DecodeString("fedcba9876543210")
	now := uint64(time.Now().UnixNano()) // #nosec G115 -- current Unix nanos are positive.
	request := &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{
							Key: "service.name",
							Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{
								StringValue: service,
							}},
						},
					},
				},
				ScopeSpans: []*tracev1.ScopeSpans{
					{
						Spans: []*tracev1.Span{
							{
								TraceId: traceID, SpanId: spanID, Name: spanName,
								StartTimeUnixNano: now, EndTimeUnixNano: now + uint64(time.Millisecond),
							},
						},
					},
				},
			},
		},
	}
	if _, err := collectortracev1.NewTraceServiceClient(connection).Export(ctx, request); err != nil {
		return fmt.Errorf("export OTLP/gRPC trace: %w", err)
	}
	return nil
}
