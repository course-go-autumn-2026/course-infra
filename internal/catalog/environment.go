package catalog

import (
	"fmt"
	"strconv"
)

// EnvVariable is one ordered line in the generated student .env file.
type EnvVariable struct {
	Name  string
	Value string
}

// Environment describes the complete normalized catalog entry for a lab.
type Environment struct {
	Lab        int
	Namespace  string
	Components []Component
	Ports      []Port
	Env        []EnvVariable
}

// IsBuiltInEnvName reports whether name is owned by the generated v1 contract.
// Names are reserved across all labs so moving a custom variable to a later lab
// cannot silently turn it into an override.
func IsBuiltInEnvName(name string) bool {
	environment, ok := EnvironmentForLab(5)
	if !ok {
		return false
	}
	for _, variable := range environment.Env {
		if variable.Name == name {
			return true
		}
	}
	return false
}

// EnvironmentForLab returns a deep copy of the complete catalog entry.
func EnvironmentForLab(lab int) (Environment, bool) {
	components, ok := Components(lab)
	if !ok {
		return Environment{}, false
	}
	namespace, _ := Namespace(lab)
	ports, _ := Ports(lab)

	port := func(name string) int {
		resolved, found := PortByName(lab, name)
		if !found {
			panic("catalog: missing endpoint " + name)
		}
		return resolved.HostPort
	}

	env := []EnvVariable{
		{Name: "HTTP_ADDR", Value: ":8080"},
		{Name: "LOG_LEVEL", Value: "info"},
		{Name: "SHUTDOWN_TIMEOUT", Value: "10s"},
		{Name: "DATABASE_URL", Value: fmt.Sprintf("postgres://tripgo:tripgo@localhost:%d/tripgo?sslmode=disable", port("postgres"))},
		{Name: "DATABASE_MAX_CONNS", Value: "10"},
		{Name: "DATABASE_MIN_CONNS", Value: "2"},
		{Name: "DATABASE_MAX_CONN_LIFETIME", Value: "30m"},
		{Name: "DATABASE_CONNECT_TIMEOUT", Value: "5s"},
		{Name: "DATABASE_QUERY_TIMEOUT", Value: "3s"},
	}
	if lab >= 2 {
		env = append(env,
			EnvVariable{Name: "OTEL_SERVICE_NAME", Value: "trip-service"},
			EnvVariable{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "http://localhost:" + strconv.Itoa(port("otel-http"))},
			EnvVariable{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "http/protobuf"},
			EnvVariable{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "service.namespace=tripgo,deployment.environment=local"},
			EnvVariable{Name: "OTEL_TRACES_SAMPLER", Value: "parentbased_always_on"},
		)
	}
	if lab >= 3 {
		env = append(env,
			EnvVariable{Name: "POSITION_CHECK_INTERVAL", Value: "30s"},
			EnvVariable{Name: "POSITION_STALE_AFTER", Value: "60s"},
			EnvVariable{Name: "POSITION_WORKERS", Value: "4"},
			EnvVariable{Name: "POSITION_QUEUE_SIZE", Value: "100"},
			EnvVariable{Name: "PUSH_HTTP_URL", Value: "http://localhost:" + strconv.Itoa(port("push-http"))},
			EnvVariable{Name: "PUSH_REQUEST_TIMEOUT", Value: "1s"},
		)
	}
	if lab >= 4 {
		env = append(env,
			EnvVariable{Name: "GRPC_ADDR", Value: ":9091"},
			EnvVariable{Name: "PUSH_GRPC_ADDR", Value: "localhost:" + strconv.Itoa(port("push-grpc"))},
			EnvVariable{Name: "BROKER_BROKERS", Value: "localhost:" + strconv.Itoa(port("redpanda-kafka"))},
			EnvVariable{Name: "TRIP_EVENTS_TOPIC", Value: "trip.events.v1"},
			EnvVariable{Name: "TRIP_COMMANDS_TOPIC", Value: "trip.commands.v1"},
			EnvVariable{Name: "TRIP_COMMANDS_DLQ_TOPIC", Value: "trip.commands.v1.dlq"},
			EnvVariable{Name: "CONSUMER_GROUP", Value: "trip-service"},
		)
	}
	if lab >= 5 {
		env = append(env,
			EnvVariable{Name: "PUSH_RETRY_MAX_ATTEMPTS", Value: "3"},
			EnvVariable{Name: "PUSH_BACKOFF_INITIAL", Value: "100ms"},
			EnvVariable{Name: "PUSH_BACKOFF_MAX", Value: "2s"},
			EnvVariable{Name: "PUSH_BACKOFF_MULTIPLIER", Value: "2.0"},
			EnvVariable{Name: "PUSH_BACKOFF_JITTER", Value: "0.2"},
			EnvVariable{Name: "PUSH_RATE_LIMIT_RPS", Value: "20"},
			EnvVariable{Name: "PUSH_RATE_LIMIT_BURST", Value: "40"},
			EnvVariable{Name: "OUTBOX_POLL_INTERVAL", Value: "1s"},
			EnvVariable{Name: "OUTBOX_BATCH_SIZE", Value: "100"},
			EnvVariable{Name: "CACHE_TTL", Value: "30s"},
			EnvVariable{Name: "CACHE_MAX_ENTRIES", Value: "10000"},
			EnvVariable{Name: "CACHE_SHARDS", Value: "16"},
			EnvVariable{Name: "WS_WRITE_TIMEOUT", Value: "5s"},
			EnvVariable{Name: "WS_PING_INTERVAL", Value: "15s"},
			EnvVariable{Name: "WS_CLIENT_BUFFER_SIZE", Value: "64"},
		)
	}

	return Environment{Lab: lab, Namespace: namespace, Components: components, Ports: ports, Env: env}, true
}
