package catalog_test

import (
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"
)

func TestPortCatalog(t *testing.T) {
	t.Parallel()

	hostPorts := make(map[int]struct{}, 50)
	nodePorts := make(map[int]struct{}, 50)
	for lab := 1; lab <= 5; lab++ {
		ports, ok := catalog.Ports(lab)
		if !ok || len(ports) != 10 {
			t.Fatalf("Ports(%d) returned %d ports, ok=%v", lab, len(ports), ok)
		}
		for _, port := range ports {
			if port.HostPort < 21000 || port.HostPort > 25999 {
				t.Errorf("host port %d is outside reserved blocks", port.HostPort)
			}
			if port.NodePort < 30000 || port.NodePort > 32767 {
				t.Errorf("node port %d is outside Kubernetes NodePort range", port.NodePort)
			}
			if _, duplicate := hostPorts[port.HostPort]; duplicate {
				t.Errorf("duplicate host port %d", port.HostPort)
			}
			if _, duplicate := nodePorts[port.NodePort]; duplicate {
				t.Errorf("duplicate node port %d", port.NodePort)
			}
			hostPorts[port.HostPort] = struct{}{}
			nodePorts[port.NodePort] = struct{}{}
		}
	}

	if catalog.LocalRegistryHostPort != 5001 || catalog.LocalRegistryInnerPort != 5000 || catalog.LocalRegistryHost != "localhost:5001" {
		t.Fatalf("unexpected local registry endpoint: %s", catalog.LocalRegistryHost)
	}
	if _, collides := hostPorts[catalog.LocalRegistryHostPort]; collides {
		t.Fatalf("local registry port %d collides with a lab endpoint", catalog.LocalRegistryHostPort)
	}

	lab4, _ := catalog.Ports(4)
	expectedHosts := []int{24081, 24092, 24032, 24300, 24317, 24318, 24686, 24809, 24905, 24909}
	for index, port := range lab4 {
		if port.HostPort != expectedHosts[index] || port.NodePort != 30401+index {
			t.Errorf("lab 4 port %q = host %d node %d", port.Name, port.HostPort, port.NodePort)
		}
	}
}

func TestExactHostNodePortAndEnvironmentMappingsLabs1Through5(t *testing.T) {
	t.Parallel()
	envValue := func(environment catalog.Environment, name string) string {
		for _, variable := range environment.Env {
			if variable.Name == name {
				return variable.Value
			}
		}
		return ""
	}
	for lab := 1; lab <= 5; lab++ {
		environment, _ := catalog.EnvironmentForLab(lab)
		for index, endpoint := range catalog.Endpoints() {
			port, ok := catalog.PortByName(lab, endpoint.Name)
			if !ok {
				t.Fatalf("lab %d endpoint %s missing", lab, endpoint.Name)
			}
			if port.HostPort != 20000+lab*1000+endpoint.HostOffset || port.NodePort != 30000+lab*100+index+1 || port.ContainerPort != endpoint.ContainerPort {
				t.Errorf("lab %d endpoint %s = host %d node %d container %d", lab, endpoint.Name, port.HostPort, port.NodePort, port.ContainerPort)
			}
		}
		postgres, _ := catalog.PortByName(lab, "postgres")
		if got, want := envValue(environment, "DATABASE_URL"), fmt.Sprintf("postgres://tripgo:tripgo@localhost:%d/tripgo?sslmode=disable", postgres.HostPort); got != want {
			t.Errorf("lab %d DATABASE_URL = %q, want %q", lab, got, want)
		}
		if lab >= 2 {
			otel, _ := catalog.PortByName(lab, "otel-http")
			if got, want := envValue(environment, "OTEL_EXPORTER_OTLP_ENDPOINT"), fmt.Sprintf("http://localhost:%d", otel.HostPort); got != want {
				t.Errorf("lab %d OTLP env = %q, want %q", lab, got, want)
			}
		}
		if lab >= 3 {
			pushHTTP, _ := catalog.PortByName(lab, "push-http")
			if got, want := envValue(environment, "PUSH_HTTP_URL"), fmt.Sprintf("http://localhost:%d", pushHTTP.HostPort); got != want {
				t.Errorf("lab %d Push HTTP env = %q, want %q", lab, got, want)
			}
		}
		if lab >= 4 {
			pushGRPC, _ := catalog.PortByName(lab, "push-grpc")
			kafka, _ := catalog.PortByName(lab, "redpanda-kafka")
			if got, want := envValue(environment, "PUSH_GRPC_ADDR"), fmt.Sprintf("localhost:%d", pushGRPC.HostPort); got != want {
				t.Errorf("lab %d Push gRPC env = %q, want %q", lab, got, want)
			}
			if got, want := envValue(environment, "BROKER_BROKERS"), fmt.Sprintf("localhost:%d", kafka.HostPort); got != want {
				t.Errorf("lab %d broker env = %q, want %q", lab, got, want)
			}
		}
	}
}

func TestImageCatalogUsesImmutableMultiArchReferences(t *testing.T) {
	t.Parallel()

	digest := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	images := catalog.Images()
	if len(images) != 9 {
		t.Fatalf("Images() returned %d entries", len(images))
	}
	for _, image := range images {
		if !slices.Contains(image.Platforms, "linux/amd64") || !slices.Contains(image.Platforms, "linux/arm64") {
			t.Errorf("image %q platforms = %v", image.Name, image.Platforms)
		}
		if image.Name == "push-service" {
			if image.Published || image.Digest != "" || image.Tag != "" || image.Reference() != "" {
				t.Errorf("unpublished Push image unexpectedly has a pull reference: %#v", image)
			}
			continue
		}
		if !image.Published || !digest.MatchString(image.Digest) {
			t.Errorf("image %q has invalid publication metadata: %#v", image.Name, image)
		}
		if image.Reference() != image.Repository+"@"+image.Digest {
			t.Errorf("image %q reference is not digest-only: %q", image.Name, image.Reference())
		}
	}
}

func TestEnvironmentCatalogIsCumulative(t *testing.T) {
	t.Parallel()

	expectedCounts := []int{9, 14, 20, 27, 42}
	for lab := 1; lab <= 5; lab++ {
		environment, ok := catalog.EnvironmentForLab(lab)
		if !ok {
			t.Fatalf("EnvironmentForLab(%d) was not found", lab)
		}
		if environment.Namespace != "tripgo-lab-0"+string(rune('0'+lab)) {
			t.Errorf("lab %d namespace = %q", lab, environment.Namespace)
		}
		if len(environment.Env) != expectedCounts[lab-1] {
			t.Errorf("lab %d has %d env variables, expected %d", lab, len(environment.Env), expectedCounts[lab-1])
		}
		if lab > 1 {
			previous, _ := catalog.EnvironmentForLab(lab - 1)
			for index := range previous.Env {
				if environment.Env[index] != previous.Env[index] && environment.Env[index].Name != "DATABASE_URL" && environment.Env[index].Name != "OTEL_EXPORTER_OTLP_ENDPOINT" && environment.Env[index].Name != "PUSH_HTTP_URL" && environment.Env[index].Name != "PUSH_GRPC_ADDR" && environment.Env[index].Name != "BROKER_BROKERS" {
					t.Errorf("lab %d changed non-port env %q", lab, environment.Env[index].Name)
				}
			}
		}
	}
}

func TestCatalogReturnsCopies(t *testing.T) {
	t.Parallel()

	components, _ := catalog.Components(4)
	components[0] = catalog.Redpanda
	again, _ := catalog.Components(4)
	if again[0] != catalog.Postgres {
		t.Fatal("Components exposed mutable catalog state")
	}

	images := catalog.Images()
	images[0].Platforms[0] = "changed"
	image, _ := catalog.ImageByName("postgres")
	if image.Platforms[0] != "linux/amd64" {
		t.Fatal("Images exposed mutable catalog state")
	}
}
