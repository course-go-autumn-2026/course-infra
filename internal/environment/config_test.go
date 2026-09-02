package environment_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/catalog"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/environment"
)

func TestParseFileValidFixtures(t *testing.T) {
	t.Parallel()

	for lab := 1; lab <= 5; lab++ {
		t.Run(fmt.Sprintf("lab-%d", lab), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("..", "..", "tests", "fixtures", "environments", fmt.Sprintf("lab-%d", lab), "environment.toml")
			config, err := environment.ParseFile(path)
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}
			expected, _ := catalog.Components(lab)
			if config.SchemaVersion != 1 || config.Lab != lab || !reflect.DeepEqual(config.Components, expected) {
				t.Fatalf("ParseFile() = %#v, expected lab %d components %v", config, lab, expected)
			}
		})
	}
}

func TestParseNormalizesComponentOrder(t *testing.T) {
	t.Parallel()

	config, err := environment.Parse("reordered.toml", []byte(`
schema_version = 1
lab = 4
components = ["redpanda", "push", "postgres", "observability"]
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	expected := []catalog.Component{catalog.Postgres, catalog.Observability, catalog.Push, catalog.Redpanda}
	if !reflect.DeepEqual(config.Components, expected) {
		t.Fatalf("components = %v, expected %v", config.Components, expected)
	}
}

func TestParseCustomEnv(t *testing.T) {
	t.Parallel()

	config, err := environment.Parse("custom.toml", []byte(`
schema_version = 1
lab = 1
components = ["postgres"]

[env]
ZEBRA = "last"
FEATURE_FLAG = "literal ${NOT_EXPANDED}"
EMPTY = ""
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	expectedCustom := []catalog.EnvVariable{
		{Name: "EMPTY", Value: ""},
		{Name: "FEATURE_FLAG", Value: "literal ${NOT_EXPANDED}"},
		{Name: "ZEBRA", Value: "last"},
	}
	if !reflect.DeepEqual(config.CustomEnv, expectedCustom) {
		t.Fatalf("CustomEnv = %#v, expected %#v", config.CustomEnv, expectedCustom)
	}
	all := config.EnvironmentVariables()
	if len(all) != 9+len(expectedCustom) || !reflect.DeepEqual(all[9:], expectedCustom) {
		t.Fatalf("EnvironmentVariables() = %#v", all)
	}
}

func TestParseDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		document  string
		field     string
		contained string
	}{
		{name: "unknown field", document: "schema_version=1\nlab=1\ncomponents=['postgres']\nimage='latest'\n", field: "image", contained: "unknown field"},
		{name: "unknown component", document: "schema_version=1\nlab=1\ncomponents=['mysql']\n", field: "components[0]", contained: "mysql"},
		{name: "incompatible schema", document: "schema_version=2\nlab=1\ncomponents=['postgres']\n", field: "schema_version", contained: "integer 1"},
		{name: "invalid lab", document: "schema_version=1\nlab=6\ncomponents=['postgres']\n", field: "lab", contained: "1 through 5"},
		{name: "duplicate component", document: "schema_version=1\nlab=2\ncomponents=['postgres','postgres']\n", field: "components[1]", contained: "duplicate"},
		{name: "lab component mismatch", document: "schema_version=1\nlab=2\ncomponents=['postgres']\n", field: "components", contained: "observability"},
		{name: "wrong type", document: "schema_version=1\nlab='one'\ncomponents=['postgres']\n", field: "lab", contained: "integer"},
		{name: "duplicate TOML key", document: "schema_version=1\nlab=1\nlab=1\ncomponents=['postgres']\n", field: "lab", contained: "already defined"},
		{name: "invalid env name", document: "schema_version=1\nlab=1\ncomponents=['postgres']\n[env]\n'BAD-NAME'='value'\n", field: "env.BAD-NAME", contained: "A-Za-z"},
		{name: "built-in env collision", document: "schema_version=1\nlab=1\ncomponents=['postgres']\n[env]\nPUSH_HTTP_URL='override'\n", field: "env.PUSH_HTTP_URL", contained: "reserved"},
		{name: "multiline env", document: "schema_version=1\nlab=1\ncomponents=['postgres']\n[env]\nMESSAGE='''first\nsecond'''\n", field: "env.MESSAGE", contained: "single-line"},
		{name: "non-string env", document: "schema_version=1\nlab=1\ncomponents=['postgres']\n[env]\nCOUNT=3\n", field: "env.COUNT", contained: "string"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			const path = "/course/task/environment.toml"
			_, err := environment.Parse(path, []byte(test.document))
			if err == nil {
				t.Fatal("Parse() error = nil")
			}
			var diagnostic *environment.ConfigError
			if !errors.As(err, &diagnostic) {
				t.Fatalf("error type = %T, expected *ConfigError", err)
			}
			if diagnostic.Path != path || diagnostic.Field != test.field {
				t.Fatalf("diagnostic = %#v, expected path %q field %q", diagnostic, path, test.field)
			}
			if !strings.Contains(err.Error(), test.contained) {
				t.Fatalf("error %q does not contain %q", err, test.contained)
			}
		})
	}
}

func TestParseFileMissing(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "environment.toml")
	_, err := environment.ParseFile(path)
	if err == nil {
		t.Fatal("ParseFile() error = nil")
	}
	var diagnostic *environment.ConfigError
	if !errors.As(err, &diagnostic) || diagnostic.Path != path || diagnostic.Field != "<file>" {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error %v does not wrap os.ErrNotExist", err)
	}
}
