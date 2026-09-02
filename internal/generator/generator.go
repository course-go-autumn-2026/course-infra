// Package generator renders deterministic Kubernetes manifests.
package generator

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/environment"
	redpandaconfig "github.com/course-go-autumn-2026/tripgo-infra/internal/redpanda"
	assettemplates "github.com/course-go-autumn-2026/tripgo-infra/templates"
)

var immutableImagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.:/_-]*@sha256:[0-9a-f]{64}$`)

// Options are trusted application inputs, never fields from environment.toml.
type Options struct {
	TripgoctlVersion   string
	ClusterID          string
	PushImageReference string
	WorkloadReplicas   *int64
}

// File is one path relative to .tripgo/rendered and its deterministic content.
type File struct {
	Name    string
	Content []byte
}

// Bundle is the complete generated manifest output for one environment.
type Bundle struct {
	Files []File
}

type model struct {
	Namespace        string
	Lab              int
	LabLabel         string
	Version          string
	ClusterID        string
	WorkloadReplicas int64
	Images           map[string]string
	Ports            map[string]catalog.Port
	RedpandaTopics   string
	ReconcileScript  string
}

// Generate renders all resources selected by a validated configuration.
func Generate(config environment.Config, options Options) (Bundle, error) {
	if options.TripgoctlVersion == "" {
		return Bundle{}, errors.New("generator: tripgoctl version is required")
	}
	if options.ClusterID == "" {
		return Bundle{}, errors.New("generator: cluster identity is required")
	}
	if config.SchemaVersion != environment.SchemaVersion {
		return Bundle{}, fmt.Errorf("generator: unsupported schema version %d", config.SchemaVersion)
	}
	entry, ok := catalog.EnvironmentForLab(config.Lab)
	if !ok {
		return Bundle{}, fmt.Errorf("generator: unsupported lab %d", config.Lab)
	}
	if !slices.Equal(config.Components, entry.Components) {
		return Bundle{}, errors.New("generator: configuration components are not normalized for the selected lab")
	}

	workloadReplicas := int64(1)
	if options.WorkloadReplicas != nil {
		workloadReplicas = *options.WorkloadReplicas
		if workloadReplicas < 0 || workloadReplicas > 1 {
			return Bundle{}, fmt.Errorf("generator: unsupported workload replica count %d", workloadReplicas)
		}
	}
	data := model{
		Namespace:        entry.Namespace,
		Lab:              config.Lab,
		LabLabel:         fmt.Sprintf("%02d", config.Lab),
		Version:          options.TripgoctlVersion,
		ClusterID:        options.ClusterID,
		WorkloadReplicas: workloadReplicas,
		Images:           make(map[string]string),
		Ports:            make(map[string]catalog.Port),
		RedpandaTopics:   redpandaconfig.Topics,
		ReconcileScript:  redpandaconfig.ReconcileScript,
	}
	for _, port := range entry.Ports {
		data.Ports[port.Name] = port
	}

	addImage := func(name string) error {
		image, found := catalog.ImageByName(name)
		if !found {
			return fmt.Errorf("generator: image %q is absent from catalog", name)
		}
		reference := image.Reference()
		if name == "push-service" && reference == "" {
			reference = options.PushImageReference
		}
		if !immutableImagePattern.MatchString(reference) {
			if name == "push-service" && reference == "" {
				return errors.New("generator: Push Service image is absent from the managed local registry; a trusted digest reference is required")
			}
			return fmt.Errorf("generator: image %q does not have a valid immutable reference", name)
		}
		if name == "push-service" && !strings.HasPrefix(reference, catalog.LocalPushImageRepository+"@sha256:") {
			return fmt.Errorf("generator: Push Service image must come from managed local repository %q", catalog.LocalPushImageRepository)
		}
		data.Images[name] = reference
		return nil
	}

	type manifest struct {
		name     string
		template string
		images   []string
	}
	manifests := []manifest{{name: "namespace.yaml", template: "namespace.yaml.tmpl"}}
	for _, component := range config.Components {
		switch component {
		case catalog.Postgres:
			manifests = append(manifests, manifest{name: "postgres.yaml", template: "postgres.yaml.tmpl", images: []string{"postgres"}})
		case catalog.Observability:
			manifests = append(manifests, manifest{name: "observability.yaml", template: "observability.yaml.tmpl", images: []string{"otel-collector", "prometheus", "grafana", "jaeger"}})
		case catalog.Push:
			manifests = append(manifests, manifest{name: "push.yaml", template: "push.yaml.tmpl", images: []string{"push-service"}})
		case catalog.Redpanda:
			manifests = append(manifests,
				manifest{name: "redpanda.yaml", template: "redpanda.yaml.tmpl", images: []string{"redpanda", "redpanda-console"}},
				manifest{name: "topics.yaml", template: "topics.yaml.tmpl"},
			)
		default:
			return Bundle{}, fmt.Errorf("generator: unsupported component %q", component)
		}
	}

	parsed, err := template.New("manifests").Funcs(template.FuncMap{
		"indent": func(spaces int, value string) string {
			prefix := strings.Repeat(" ", spaces)
			return prefix + strings.ReplaceAll(strings.TrimSuffix(value, "\n"), "\n", "\n"+prefix) + "\n"
		},
	}).Option("missingkey=error").ParseFS(assettemplates.Files, "base/*.yaml.tmpl", "components/*.yaml.tmpl")
	if err != nil {
		return Bundle{}, fmt.Errorf("generator: parse embedded templates: %w", err)
	}

	files := make([]File, 0, len(manifests))
	for _, item := range manifests {
		for _, image := range item.images {
			if _, exists := data.Images[image]; !exists {
				if err := addImage(image); err != nil {
					return Bundle{}, err
				}
			}
		}
		var rendered bytes.Buffer
		if err := parsed.ExecuteTemplate(&rendered, item.template, data); err != nil {
			return Bundle{}, fmt.Errorf("generator: render %s: %w", item.name, err)
		}
		content := rendered.Bytes()
		if len(content) == 0 || content[len(content)-1] != '\n' {
			content = append(content, '\n')
		}
		files = append(files, File{Name: item.name, Content: append([]byte(nil), content...)})
	}

	return Bundle{Files: files}, nil
}
