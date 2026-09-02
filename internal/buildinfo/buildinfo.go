// Package buildinfo provides build metadata shared by the repository binaries.
package buildinfo

import "fmt"

var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

// Info describes one built artifact.
type Info struct {
	Name    string
	Version string
	Commit  string
	BuiltAt string
}

// Current returns metadata injected with build linker flags.
func Current(name string) Info {
	return Info{
		Name:    name,
		Version: version,
		Commit:  commit,
		BuiltAt: builtAt,
	}
}

// String returns the public one-line version representation.
func (i Info) String() string {
	return fmt.Sprintf("%s %s (commit %s, built %s)", i.Name, i.Version, i.Commit, i.BuiltAt)
}
