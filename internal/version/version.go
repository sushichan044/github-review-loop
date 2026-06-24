// Package version provides version information of the application. It uses Go's build info to determine the version, VCS revision, and modification status.
// Version information is automatically embedded at build time using Go's build info.
package version

import (
	"fmt"
	"runtime/debug"
)

const (
	// gitShortHashLength is the standard length for git short hashes (7 characters).
	gitShortHashLength = 7
)

// version can be set at build time via ldflags: -X github.com/sushichan044/github-review-loop/internal/version.version=vX.Y.Z.
var version string

// Get returns the version information of the application.
// It reads version from [runtime/debug.ReadBuildInfo]() which is automatically
// populated when built with Go modules and version tags.
//
// The version format is:
//   - "vX.Y.Z" when built from a tagged release
//   - "dev" when built locally without version info
//   - "vX.Y.Z (rev: abc1234)" when built with VCS revision info
//   - "vX.Y.Z (rev: abc1234, modified)" when built with uncommitted changes
func Get() string {
	info, ok := debug.ReadBuildInfo()

	v := version
	if v == "" {
		if !ok {
			return "unknown"
		}
		v = info.Main.Version
		if v == "" || v == "(devel)" {
			v = "dev"
		}
	}

	if !ok {
		return v
	}

	return formatWithVCS(v, info.Settings)
}

func formatWithVCS(v string, settings []debug.BuildSetting) string {
	var revision string
	var modified bool

	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}

	if revision == "" {
		return v
	}

	if len(revision) > gitShortHashLength {
		revision = revision[:gitShortHashLength]
	}

	dirty := ""
	if modified {
		dirty = ", modified"
	}

	return fmt.Sprintf("%s (rev: %s%s)", v, revision, dirty)
}
