package github_test

import (
	"testing"

	"github.com/sushichan044/github-review-loop/internal/github"
)

// TestNewClientSignature verifies NewClient is callable and returns the expected types.
// The actual connection requires gh CLI credentials so we only assert the signature.
func TestNewClientSignature(t *testing.T) {
	t.Parallel()
	// NewClient requires a live gh CLI auth token; skip in environments without one.
	if testing.Short() {
		t.Skip("requires gh CLI authentication")
	}
	client, err := github.NewClient()
	if err != nil {
		t.Skipf("no gh auth available: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
