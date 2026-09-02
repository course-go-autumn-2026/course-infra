package buildinfo_test

import (
	"testing"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/buildinfo"
)

func TestInfoString(t *testing.T) {
	t.Parallel()

	info := buildinfo.Info{
		Name:    "tripgoctl",
		Version: "v1.2.3",
		Commit:  "abcdef1",
		BuiltAt: "2026-09-01T00:00:00Z",
	}

	want := "tripgoctl v1.2.3 (commit abcdef1, built 2026-09-01T00:00:00Z)"
	if got := info.String(); got != want {
		t.Fatalf("Info.String() = %q, want %q", got, want)
	}
}
