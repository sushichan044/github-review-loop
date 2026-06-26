package cmd

import (
	"bytes"
	"context"
	"io"

	"github.com/sushichan044/mergeable-please/internal/backend"
	"github.com/sushichan044/mergeable-please/internal/backend/github"
	"github.com/sushichan044/mergeable-please/internal/config"
	"github.com/sushichan044/mergeable-please/internal/core"
	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
	"github.com/sushichan044/mergeable-please/internal/output"
)

// TestDeps is the public surface for injecting test doubles into cmd tests.
type TestDeps struct {
	Resolver         github.PRResolver
	BundledEvaluate  func(ctx context.Context, pr github.PR) (core.CheckResult, error)
	FetchSnapshot    func(ctx context.Context, pr github.PR, policies []reviewer.Policy) (reviewer.Snapshot, error)
	ThreadComments   func(ctx context.Context, pr github.PR, policies []reviewer.Policy) (map[string][]github.ThreadComment, error)
	FetchBranchRules func(ctx context.Context, pr github.PR) ([]backend.BranchRule, error)
	Triggerer        *github.Triggerer
	LoadConfig       func() (*config.Config, error)
	InitConfig       func() (string, error)
	Out              io.Writer
}

func toDeps(td TestDeps) deps {
	triggerer := td.Triggerer
	if triggerer == nil {
		triggerer = github.NewTriggererWithExec(func(_ ...string) (bytes.Buffer, bytes.Buffer, error) {
			return bytes.Buffer{}, bytes.Buffer{}, nil
		})
	}

	threadComments := td.ThreadComments
	if threadComments == nil {
		threadComments = func(_ context.Context, _ github.PR, _ []reviewer.Policy) (map[string][]github.ThreadComment, error) {
			return map[string][]github.ThreadComment{}, nil
		}
	}

	fetchSnapshot := td.FetchSnapshot
	if fetchSnapshot == nil {
		fetchSnapshot = func(_ context.Context, _ github.PR, _ []reviewer.Policy) (reviewer.Snapshot, error) {
			return reviewer.Snapshot{}, nil
		}
	}

	bundledEvaluate := td.BundledEvaluate
	if bundledEvaluate == nil {
		bundledEvaluate = func(_ context.Context, _ github.PR) (core.CheckResult, error) {
			return core.CheckResult{}, nil
		}
	}

	return deps{
		resolver:         td.Resolver,
		bundledEvaluate:  bundledEvaluate,
		fetchSnapshot:    fetchSnapshot,
		threadComments:   threadComments,
		fetchBranchRules: td.FetchBranchRules,
		triggerer:        triggerer,
		loadConfig:       td.LoadConfig,
		initConfig:       td.InitConfig,
		out:              td.Out,
	}
}

// RunCheckForTest executes the check command with the given test deps and format string.
// args are the positional PR arguments (0 or 1 element).
func RunCheckForTest(
	ctx context.Context,
	td TestDeps,
	formatStr string,
	args []string,
) error {
	d := toDeps(td)
	resolveFormat := formatResolver(func() (output.Format, error) {
		if formatStr == "" {
			return output.DefaultFormat(), nil
		}
		return output.ParseFormat(formatStr)
	})
	return runCheck(ctx, d, resolveFormat, args)
}

// RunRequestForTest executes the request command with the given test deps.
func RunRequestForTest(
	ctx context.Context,
	td TestDeps,
	reviewerFlag string,
	args []string,
) error {
	d := toDeps(td)
	resolveFormat := formatResolver(func() (output.Format, error) {
		return output.DefaultFormat(), nil
	})
	return runRequest(ctx, d, resolveFormat, reviewerFlag, args)
}

// RunInitForTest executes the init command with the given test deps.
func RunInitForTest(td TestDeps) error {
	return runInit(toDeps(td))
}
