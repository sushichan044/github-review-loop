package version_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sushichan044/go-template/internal/version"
)

func TestGet(t *testing.T) {
	t.Parallel()
	v := version.Get()

	assert.NotEmpty(t, v)
}
