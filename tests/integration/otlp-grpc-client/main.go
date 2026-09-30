// Command otlp-grpc-client sends a correlated trace and log through an OTLP/gRPC endpoint.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"time"

	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	endpoint := flag.String("endpoint", "localhost:22317", "OTLP/gRPC endpoint")
	service := flag.String("service", "tripgo-integration-grpc", "service.name resource attribute")
	spanName := flag.String("span", "integration-grpc-trace", "span name")
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
	response, err := collectortracev1.NewTraceServiceClient(connection).Export(ctx, request)
	if err != nil {
		return fmt.Errorf("export OTLP/gRPC trace: %w", err)
	}
	if response.GetPartialSuccess().GetRejectedSpans() != 0 {
		return fmt.Errorf("OTLP/gRPC trace rejected: %s", response.GetPartialSuccess().GetErrorMessage())
	}
	logs, err := collectorlogsv1.NewLogsServiceClient(connection).Export(ctx, &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{
			Resource: request.ResourceSpans[0].Resource,
			ScopeLogs: []*logsv1.ScopeLogs{{
				LogRecords: []*logsv1.LogRecord{{
					TimeUnixNano: now, SeverityNumber: logsv1.SeverityNumber_SEVERITY_NUMBER_INFO,
					SeverityText: "INFO", TraceId: traceID, SpanId: spanID,
					Body: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: spanName}},
				}},
			}},
		}},
	})
	if err != nil {
		return fmt.Errorf("export OTLP/gRPC log: %w", err)
	}
	if logs.GetPartialSuccess().GetRejectedLogRecords() != 0 {
		return fmt.Errorf("OTLP/gRPC log rejected: %s", logs.GetPartialSuccess().GetErrorMessage())
	}
	return nil
}
