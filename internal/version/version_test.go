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
