package cluster

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"
)

func kindConfig() ([]byte, string, error) {
	var config strings.Builder
	fmt.Fprintln(&config, "kind: Cluster")
	fmt.Fprintln(&config, "apiVersion: kind.x-k8s.io/v1alpha4")
	fmt.Fprintln(&config, "name: "+Name)
	fmt.Fprintln(&config, "networking:")
	fmt.Fprintln(&config, "  apiServerAddress: 127.0.0.1")
	fmt.Fprintln(&config, "containerdConfigPatches:")
	fmt.Fprintln(&config, "  - |-")
	fmt.Fprintf(&config, "    [plugins.\"io.containerd.grpc.v1.cri\".registry.mirrors.\"%s\"]\n", catalog.LocalRegistryHost)
	fmt.Fprintf(&config, "      endpoint = [\"http://%s:%d\"]\n", RegistryContainer, catalog.LocalRegistryInnerPort)
	fmt.Fprintln(&config, "nodes:")
	fmt.Fprintln(&config, "  - role: control-plane")
	fmt.Fprintln(&config, "    image: "+KindNodeImage)
	fmt.Fprintln(&config, "    extraPortMappings:")
	for lab := 1; lab <= 5; lab++ {
		ports, ok := catalog.Ports(lab)
		if !ok {
			return nil, "", fmt.Errorf("cluster: catalog has no ports for lab %d", lab)
		}
		for _, port := range ports {
			fmt.Fprintf(&config, "      - containerPort: %d\n", port.NodePort)
			fmt.Fprintf(&config, "        hostPort: %d\n", port.HostPort)
			fmt.Fprintln(&config, "        listenAddress: 127.0.0.1")
			fmt.Fprintln(&config, "        protocol: TCP")
		}
	}
	content := []byte(config.String())
	return content, fmt.Sprintf("%x", sha256.Sum256(content)), nil
}
