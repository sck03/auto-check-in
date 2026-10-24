package buildinfo

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var defaultVersion string

// Version may be supplied by the release linker; ordinary builds use VERSION.
var Version string

func init() {
	if Version == "" {
		Version = strings.TrimSpace(defaultVersion)
	}
}
