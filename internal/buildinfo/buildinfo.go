// Package buildinfo reports the version of the running binary.
package buildinfo

import "runtime/debug"

// version is set at build time:
//
//	go build -ldflags=-X=github.com/uhiddename/reloc/internal/buildinfo.version=v1.2.3
var version string

// Version returns the version of the running binary.
func Version() string {
	if version != "" {
		return version // set by -X: Docker, CI
	}
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version // go install ...@v1.2.3, or go build in a git checkout
	}
	return "dev"
}
