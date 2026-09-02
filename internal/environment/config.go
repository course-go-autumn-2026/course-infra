// Package environment parses the public environment.toml contract.
package environment

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"
)

// SchemaVersion is the only public environment.toml schema accepted by this release.
const SchemaVersion = 1

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Config is a validated and normalized environment.toml. Components use
// catalog order and CustomEnv uses lexical name order, independent of source
// order.
type Config struct {
	SchemaVersion int
	Lab           int
	Components    []catalog.Component
	CustomEnv     []catalog.EnvVariable
}

// EnvironmentVariables returns built-in lab variables followed by custom
// variables. Validation guarantees that names cannot collide.
func (c Config) EnvironmentVariables() []catalog.EnvVariable {
	entry, ok := catalog.EnvironmentForLab(c.Lab)
	if !ok {
		return nil
	}
	result := append([]catalog.EnvVariable(nil), entry.Env...)
	return append(result, c.CustomEnv...)
}

type document struct {
	SchemaVersion int               `toml:"schema_version"`
	Lab           int               `toml:"lab"`
	Components    []string          `toml:"components"`
	Env           map[string]string `toml:"env"`
}

// ConfigError is a diagnostic at the public configuration boundary.
type ConfigError struct {
	Path     string
	Field    string
	Expected string
	Actual   string
	Line     int
	Column   int
	Cause    error
}

func (e *ConfigError) Error() string {
	location := e.Path
	if e.Line > 0 {
		location += fmt.Sprintf(":%d:%d", e.Line, e.Column)
	}
	message := fmt.Sprintf("%s: field %q: expected %s", location, e.Field, e.Expected)
	if e.Actual != "" {
		message += ", got " + e.Actual
	}
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	return message
}

func (e *ConfigError) Unwrap() error { return e.Cause }

// ParseFile reads and validates an environment.toml.
func ParseFile(path string) (Config, error) {
	// #nosec G304 -- path is the explicit application-layer input; v1 callers pass ./environment.toml.
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, &ConfigError{
			Path: path, Field: "<file>", Expected: "a readable environment.toml", Cause: err,
		}
	}
	return Parse(path, data)
}

// Parse validates TOML bytes and returns a normalized configuration.
func Parse(path string, data []byte) (Config, error) {
	var decoded document
	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return Config{}, decodeError(path, err)
	}

	if decoded.SchemaVersion != SchemaVersion {
		return Config{}, validationError(path, "schema_version", "integer 1", fmt.Sprint(decoded.SchemaVersion))
	}
	if _, ok := catalog.Components(decoded.Lab); !ok {
		return Config{}, validationError(path, "lab", "an integer from 1 through 5", fmt.Sprint(decoded.Lab))
	}
	if len(decoded.Components) == 0 {
		return Config{}, validationError(path, "components", "a non-empty array", "empty array")
	}

	seen := make(map[string]struct{}, len(decoded.Components))
	for index, name := range decoded.Components {
		field := fmt.Sprintf("components[%d]", index)
		if !catalog.IsComponent(name) {
			return Config{}, validationError(path, field, "one of postgres, observability, push, redpanda", fmt.Sprintf("%q", name))
		}
		if _, exists := seen[name]; exists {
			return Config{}, validationError(path, field, "a component that is not already listed", fmt.Sprintf("duplicate %q", name))
		}
		seen[name] = struct{}{}
	}

	customEnv := make([]catalog.EnvVariable, 0, len(decoded.Env))
	for name, value := range decoded.Env {
		field := "env." + name
		if !envNamePattern.MatchString(name) {
			return Config{}, validationError(path, field, "a name matching [A-Za-z_][A-Za-z0-9_]*", fmt.Sprintf("%q", name))
		}
		if catalog.IsBuiltInEnvName(name) {
			return Config{}, validationError(path, field, "a name that does not override a built-in variable", fmt.Sprintf("reserved name %q", name))
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return Config{}, validationError(path, field, "a literal single-line string without NUL", fmt.Sprintf("%q", value))
		}
		customEnv = append(customEnv, catalog.EnvVariable{Name: name, Value: value})
	}
	slices.SortFunc(customEnv, func(left, right catalog.EnvVariable) int {
		return strings.Compare(left.Name, right.Name)
	})

	expected, _ := catalog.Components(decoded.Lab)
	expectedNames := componentNames(expected)
	actualNames := append([]string(nil), decoded.Components...)
	slices.Sort(actualNames)
	expectedSorted := append([]string(nil), expectedNames...)
	slices.Sort(expectedSorted)
	if !slices.Equal(actualNames, expectedSorted) {
		return Config{}, validationError(
			path,
			"components",
			fmt.Sprintf("set %q for lab %d", expectedNames, decoded.Lab),
			fmt.Sprintf("set %q", decoded.Components),
		)
	}

	return Config{SchemaVersion: decoded.SchemaVersion, Lab: decoded.Lab, Components: expected, CustomEnv: customEnv}, nil
}

func decodeError(path string, err error) error {
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) && len(strict.Errors) > 0 {
		first := strict.Errors[0]
		line, column := first.Position()
		return &ConfigError{
			Path: path, Field: keyName(first.Key()), Expected: "only schema_version, lab, components, and env fields",
			Line: line, Column: column, Cause: errors.New("unknown field"),
		}
	}

	var decoded *toml.DecodeError
	if errors.As(err, &decoded) {
		line, column := decoded.Position()
		return &ConfigError{
			Path: path, Field: keyName(decoded.Key()), Expected: expectedType(keyName(decoded.Key())),
			Line: line, Column: column, Cause: err,
		}
	}

	return &ConfigError{Path: path, Field: "<document>", Expected: "valid TOML", Cause: err}
}

func keyName(key toml.Key) string {
	if len(key) == 0 {
		return "<document>"
	}
	return strings.Join(key, ".")
}

func expectedType(field string) string {
	switch field {
	case "schema_version", "lab":
		return "an integer"
	case "components":
		return "an array of strings"
	default:
		return "valid TOML"
	}
}

func validationError(path, field, expected, actual string) error {
	return &ConfigError{Path: path, Field: field, Expected: expected, Actual: actual}
}

func componentNames(components []catalog.Component) []string {
	names := make([]string, len(components))
	for index, component := range components {
		names[index] = string(component)
	}
	return names
}
