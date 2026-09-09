package catalog

// LocalPushImageRepository is served by the managed registry configured for kind.
const LocalPushImageRepository = "localhost:5001/tripgo-push-service"

// Image is an OCI artifact selected by the generator. A published image is
// always consumed by digest; Tag remains as human-readable provenance.
type Image struct {
	Name       string
	Repository string
	Tag        string
	Digest     string
	Platforms  []string
	Published  bool
}

// Reference returns the immutable pull reference, or an empty string for an
// artifact that has not been published yet.
func (i Image) Reference() string {
	if !i.Published || i.Digest == "" {
		return ""
	}
	return i.Repository + "@" + i.Digest
}

var images = []Image{
	{Name: "postgres", Repository: "docker.io/library/postgres", Tag: "16.10-alpine", Digest: "sha256:029660641a0cfc575b14f336ba448fb8a75fd595d42e1fa316b9fb4378742297", Platforms: []string{"linux/amd64", "linux/arm64"}, Published: true},
	{Name: "otel-collector", Repository: "docker.io/otel/opentelemetry-collector-contrib", Tag: "0.159.0", Digest: "sha256:1f2c54a30e713fac6b3ae77a1ec84010c2007e29ced8ec666214fc2f6739c1cc", Platforms: []string{"linux/amd64", "linux/arm64"}, Published: true},
	{Name: "prometheus", Repository: "docker.io/prom/prometheus", Tag: "v3.14.0", Digest: "sha256:5ce7540c3c00ef4ab0c9d2c995c6a5b9c421f44b4a115d97a2c7af3b1c21cbb0", Platforms: []string{"linux/amd64", "linux/arm64"}, Published: true},
	{Name: "grafana", Repository: "docker.io/grafana/grafana", Tag: "13.2.0", Digest: "sha256:3fd54ae1214669f8355f065ec9f6445d5279a3d77095ab048ca045685272429b", Platforms: []string{"linux/amd64", "linux/arm64"}, Published: true},
	{Name: "jaeger", Repository: "docker.io/jaegertracing/jaeger", Tag: "2.20.0", Digest: "sha256:46a886260e04002d8f45e213fc39063fa11a50446048fdaa64786fc0840cb9f8", Platforms: []string{"linux/amd64", "linux/arm64"}, Published: true},
	{Name: "redpanda", Repository: "docker.io/redpandadata/redpanda", Tag: "v26.2.2", Digest: "sha256:468bd13a9f2bd24794cb7fddc867c767fb1008b9a07b297b89fde48c564d7d96", Platforms: []string{"linux/amd64", "linux/arm64"}, Published: true},
	{Name: "redpanda-console", Repository: "docker.redpanda.com/redpandadata/console", Tag: "v3.11.0", Digest: "sha256:bf5ed3ee83e5b2dd6d1c1f1c8095ba252f384ce9354e7a3d2f780f0b4a486c09", Platforms: []string{"linux/amd64", "linux/arm64"}, Published: true},
	{Name: "registry", Repository: "docker.io/library/registry", Tag: "2.8.3", Digest: "sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373", Platforms: []string{"linux/amd64", "linux/arm64"}, Published: true},
	{Name: "push-service", Repository: LocalPushImageRepository, Platforms: []string{"linux/amd64", "linux/arm64"}, Published: false},
}

// ImageByName looks up an image by its stable catalog name.
func ImageByName(name string) (Image, bool) {
	for _, image := range images {
		if image.Name == name {
			image.Platforms = append([]string(nil), image.Platforms...)
			return image, true
		}
	}
	return Image{}, false
}
