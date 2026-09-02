package catalog

// Fixed v1 port-allocation constants.
const (
	HostPortBase           = 20000
	HostPortLabStep        = 1000
	NodePortBase           = 30000
	NodePortLabStep        = 100
	HostListenAddress      = "127.0.0.1"
	LocalRegistryHost      = "localhost:5001"
	LocalRegistryHostPort  = 5001
	LocalRegistryInnerPort = 5000
)

// Endpoint describes one stable host-to-NodePort mapping. Order is part of the
// v1 cluster contract and determines NodePortOffset.
type Endpoint struct {
	Name           string
	Component      Component
	HostOffset     int
	NodePortOffset int
	ContainerPort  int
}

// Port is an endpoint resolved for a particular lab.
type Port struct {
	Endpoint
	HostPort int
	NodePort int
}

var endpoints = []Endpoint{
	{Name: "redpanda-console", Component: Redpanda, HostOffset: 81, NodePortOffset: 1, ContainerPort: 8080},
	{Name: "redpanda-kafka", Component: Redpanda, HostOffset: 92, NodePortOffset: 2, ContainerPort: 9092},
	{Name: "postgres", Component: Postgres, HostOffset: 32, NodePortOffset: 3, ContainerPort: 5432},
	{Name: "grafana", Component: Observability, HostOffset: 300, NodePortOffset: 4, ContainerPort: 3000},
	{Name: "otel-grpc", Component: Observability, HostOffset: 317, NodePortOffset: 5, ContainerPort: 4317},
	{Name: "otel-http", Component: Observability, HostOffset: 318, NodePortOffset: 6, ContainerPort: 4318},
	{Name: "jaeger", Component: Observability, HostOffset: 686, NodePortOffset: 7, ContainerPort: 16686},
	{Name: "push-http", Component: Push, HostOffset: 809, NodePortOffset: 8, ContainerPort: 8090},
	{Name: "push-grpc", Component: Push, HostOffset: 905, NodePortOffset: 9, ContainerPort: 9095},
	{Name: "prometheus", Component: Observability, HostOffset: 909, NodePortOffset: 10, ContainerPort: 9090},
}

// Endpoints returns all mappings reserved for every lab.
func Endpoints() []Endpoint {
	return append([]Endpoint(nil), endpoints...)
}

// Ports resolves all reserved mappings for a lab. Cluster creation uses all ten
// mappings even when the lab does not deploy every component.
func Ports(lab int) ([]Port, bool) {
	if _, ok := componentsByLab[lab]; !ok {
		return nil, false
	}
	ports := make([]Port, 0, len(endpoints))
	for _, endpoint := range endpoints {
		ports = append(ports, Port{
			Endpoint: endpoint,
			HostPort: HostPortBase + lab*HostPortLabStep + endpoint.HostOffset,
			NodePort: NodePortBase + lab*NodePortLabStep + endpoint.NodePortOffset,
		})
	}
	return ports, true
}

// PortByName resolves one endpoint for a lab.
func PortByName(lab int, name string) (Port, bool) {
	ports, ok := Ports(lab)
	if !ok {
		return Port{}, false
	}
	for _, port := range ports {
		if port.Name == name {
			return port, true
		}
	}
	return Port{}, false
}
