package version_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/mod/semver"

	"github.com/sushichan044/mergeable-please/internal/version"
)

func TestGet(t *testing.T) {
	t.Parallel()
	v := version.Get()

	assert.NotEmpty(t, v)
}

// TestSemverIsValid guards the contract skillsmith relies on: Semver always
// returns a strict semver string (with a "v" prefix), even for untagged builds.
func TestSemverIsValid(t *testing.T) {
	t.Parallel()
	v := version.Semver()

	assert.True(t, semver.IsValid(v), "Semver() must return valid semver, got %q", v)
}

func TestNormalizeSemver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "already valid with v prefix",
			input: "v1.2.3",
			want:  "v1.2.3",
		},
		{
			name:  "missing v prefix",
			input: "1.2.3",
			want:  "v1.2.3",
		},
		{
			name:  "pre-release with v prefix",
			input: "v1.2.3-rc.1",
			want:  "v1.2.3-rc.1",
		},
		{
			name:  "pre-release without v prefix",
			input: "1.2.3-rc.1",
			want:  "v1.2.3-rc.1",
		},
		{
			name:  "empty string falls back",
			input: "",
			want:  "v0.0.0-dev",
		},
		{
			name:  "devel pseudo-version falls back",
			input: "(devel)",
			want:  "v0.0.0-dev",
		},
		{
			name:  "dev string falls back",
			input: "dev",
			want:  "v0.0.0-dev",
		},
		{
			name:  "garbage falls back",
			input: "garbage",
			want:  "v0.0.0-dev",
		},
		{
			name:  "build metadata preserved",
			input: "v1.2.3+dirty",
			want:  "v1.2.3+dirty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := version.NormalizeSemver(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
