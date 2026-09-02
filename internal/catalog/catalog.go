// Package catalog contains the immutable infrastructure contract compiled into tripgoctl.
package catalog

import "fmt"

// Component is a top-level capability selected by environment.toml.
type Component string

// Public component names in schema v1.
const (
	Postgres      Component = "postgres"
	Observability Component = "observability"
	Push          Component = "push"
	Redpanda      Component = "redpanda"
)

var componentsByLab = map[int][]Component{
	1: {Postgres},
	2: {Postgres, Observability},
	3: {Postgres, Observability, Push},
	4: {Postgres, Observability, Push, Redpanda},
	5: {Postgres, Observability, Push, Redpanda},
}

// Components returns the canonical component order for a lab.
func Components(lab int) ([]Component, bool) {
	components, ok := componentsByLab[lab]
	if !ok {
		return nil, false
	}
	return append([]Component(nil), components...), true
}

// IsComponent reports whether name is part of the public v1 vocabulary.
func IsComponent(name string) bool {
	switch Component(name) {
	case Postgres, Observability, Push, Redpanda:
		return true
	default:
		return false
	}
}

// Namespace returns the namespace reserved for a lab.
func Namespace(lab int) (string, bool) {
	if _, ok := componentsByLab[lab]; !ok {
		return "", false
	}
	return fmt.Sprintf("tripgo-lab-%02d", lab), true
}
