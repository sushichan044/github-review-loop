package cmd

import (
	"bytes"
	"context"
	"io"

	"github.com/sushichan044/github-review-loop/internal/config"
	"github.com/sushichan044/github-review-loop/internal/github"
	"github.com/sushichan044/github-review-loop/internal/output"
	"github.com/sushichan044/github-review-loop/internal/reviewloop"
)

// TestDeps is the public surface for injecting test doubles into cmd tests.
type TestDeps struct {
	Resolver       github.PRResolver
	FetchSnapshot  func(ctx context.Context, pr github.PR, policies []reviewloop.Policy) (reviewloop.Snapshot, error)
	ThreadComments func(ctx context.Context, pr github.PR, policies []reviewloop.Policy) (map[string][]github.ThreadComment, error)
	Triggerer      *github.Triggerer
	LoadConfig     func() (*config.Config, error)
	InitConfig     func() (string, error)
	Out            io.Writer
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
		threadComments = func(_ context.Context, _ github.PR, _ []reviewloop.Policy) (map[string][]github.ThreadComment, error) {
			return map[string][]github.ThreadComment{}, nil
		}
	}

	return deps{
		resolver:       td.Resolver,
		fetchSnapshot:  td.FetchSnapshot,
		threadComments: threadComments,
		triggerer:      triggerer,
		loadConfig:     td.LoadConfig,
		initConfig:     td.InitConfig,
		out:            td.Out,
	}
}

// RunStatusForTest executes the status command with the given test deps and format string.
// args are the positional PR arguments (0 or 1 element).
func RunStatusForTest(
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

	return runStatus(ctx, d, resolveFormat, args)
}

// RunRequestForTest executes the request command with the given test deps and format string.
// args are the positional PR arguments (0 or 1 element).
func RunRequestForTest(
	ctx context.Context,
	td TestDeps,
	reviewerFlag string,
	args []string,
) error {
	d := toDeps(td)
	// request doesn't use format for output, but validates the flag.
	resolveFormat := formatResolver(func() (output.Format, error) {
		return output.DefaultFormat(), nil
	})

	return runRequest(ctx, d, resolveFormat, reviewerFlag, args)
}

// RunInitForTest executes the init command with the given test deps.
func RunInitForTest(td TestDeps) error {
	return runInit(toDeps(td))
}
