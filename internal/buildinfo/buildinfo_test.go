package buildinfo

import "testing"

// setVersion stands in for -ldflags=-X until the test ends. Tests that use
// it change a package variable and must not run in parallel.
func setVersion(t *testing.T, v string) {
	t.Helper()
	prev := version
	version = v
	t.Cleanup(func() { version = prev })
}

func TestVersionFromLinker(t *testing.T) {
	setVersion(t, "6f579e0d")
	if got := Version(); got != "6f579e0d" {
		t.Errorf("Version() = %q, want the value set by -X", got)
	}
}

// A test binary is built without VCS stamping, so its build info says
// "(devel)": the same as a build without .git, inside Docker.
func TestVersionDev(t *testing.T) {
	setVersion(t, "")
	if got := Version(); got != "dev" {
		t.Errorf("Version() = %q, want dev", got)
	}
}
