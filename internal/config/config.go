// Package config loads, parses, and resolves the mergeable-please config file.
//
// Config lives in the repository it applies to, at .github/review-loop.yml
// (a .yaml extension is also accepted). Committing it is how a review-loop
// policy is shared with collaborators and CI — there is no machine-global
// config, because a per-machine file cannot enforce policy across a team.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	z "github.com/Oudwins/zog"
	"gopkg.in/yaml.v3"

	"github.com/sushichan044/mergeable-please/internal/core/reviewer"
)

const (
	configDir         = ".github"
	defaultConfigName = "review-loop.yml"
	defaultMaxRallies = 5
)

// configNames are the accepted config file names, tried in order. Both .yml and
// .yaml are accepted so users are not tripped up by the extension; init writes
// defaultConfigName.
func configNames() []string {
	return []string{defaultConfigName, "review-loop.yaml"}
}

// defaultConfig is the commented template written by Init.
//
//go:embed templates/review-loop.yml
var defaultConfig []byte

// ErrConfigNotFound is returned by Load when no config file exists for the
// current repository (including when the working directory is not inside a
// git repository). Callers treat this as "not configured yet" rather than a
// hard failure.
var ErrConfigNotFound = errors.New("config: file not found")

// ErrConfigExists is returned by Init when a config file already exists, so it
// never clobbers a committed policy.
var ErrConfigExists = errors.New("config: file already exists")

// GoalConfig holds the two mutually exclusive goal flags as parsed from YAML.
type GoalConfig struct {
	Approved                 bool `zog:"approved"`
	AllConversationsResolved bool `zog:"all-conversations-resolved"`
}

// ReviewerConfig is a single reviewer entry.
type ReviewerConfig struct {
	Type       string     `zog:"type"`
	Name       string     `zog:"name"`
	Goal       GoalConfig `zog:"goal"`
	MaxRallies int        `zog:"max-rallies"`
	Trigger    string     `zog:"trigger"`
}

// Config is the parsed, validated representation of review-loop.yml.
type Config struct {
	Reviewers []ReviewerConfig `zog:"reviewers"`
}

func buildSchema() *z.StructSchema {
	goalSchema := z.Struct(z.Shape{
		"Approved":                 z.Bool(),
		"AllConversationsResolved": z.Bool(),
	}).TestFunc(func(dataPtr any, ctx z.Ctx) bool {
		g, ok := dataPtr.(*GoalConfig)
		if !ok {
			return false
		}
		if g.Approved == g.AllConversationsResolved {
			ctx.AddIssue(
				ctx.Issue().SetMessage("exactly one of 'approved' or 'all-conversations-resolved' must be true"),
			)
			return false
		}
		return true
	})

	reviewerSchema := z.Struct(z.Shape{
		"Type":       z.String().OneOf([]string{"user", "github-copilot", "github-app"}),
		"Name":       z.String(),
		"Goal":       goalSchema,
		"MaxRallies": z.Int().Default(defaultMaxRallies),
		"Trigger":    z.String(),
	}).TestFunc(func(dataPtr any, ctx z.Ctx) bool {
		r, ok := dataPtr.(*ReviewerConfig)
		if !ok {
			return false
		}
		if (r.Type == "user" || r.Type == "github-app") && r.Name == "" {
			ctx.AddIssue(ctx.Issue().SetMessage(fmt.Sprintf("'name' is required for reviewer type '%s'", r.Type)))
			return false
		}
		return true
	})

	return z.Struct(z.Shape{
		"Reviewers": z.Slice(reviewerSchema),
	})
}

// Parse unmarshals YAML data and validates it with zog.
func Parse(data []byte) (*Config, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("config: yaml parse error: %w", err)
	}

	var cfg Config
	if errs := buildSchema().Parse(raw, &cfg); errs != nil {
		return nil, fmt.Errorf("config: validation error: %v", errs)
	}
	return &cfg, nil
}

// Load finds the config for the current repository and parses it. It walks up
// from the working directory to the repository root, then reads
// .github/review-loop.yml (or .yaml). It returns ErrConfigNotFound when no such
// file exists, or when the working directory is not inside a git repository.
func Load() (*Config, error) {
	start, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("config: cannot determine working directory: %w", err)
	}
	return loadFrom(start)
}

func loadFrom(start string) (*Config, error) {
	root, err := repoRootFrom(start)
	if err != nil {
		// Not inside a git repo => there is no repo-local config to find.
		return nil, fmt.Errorf("%w: %w", ErrConfigNotFound, err)
	}

	for _, name := range configNames() {
		path := filepath.Join(root, configDir, name)
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			return Parse(data)
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return nil, fmt.Errorf("config: cannot read %s: %w", path, readErr)
		}
	}

	return nil, fmt.Errorf("%w under %s", ErrConfigNotFound, filepath.Join(root, configDir))
}

// Init writes the default config template to .github/review-loop.yml at the
// current repository root, creating .github if needed. It returns the path
// written. It returns ErrConfigExists if a config file already exists, so a
// committed policy is never overwritten.
func Init() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("config: cannot determine working directory: %w", err)
	}
	return initAt(start)
}

func initAt(start string) (string, error) {
	root, err := repoRootFrom(start)
	if err != nil {
		return "", err
	}

	dir := filepath.Join(root, configDir)
	for _, name := range configNames() {
		existing := filepath.Join(dir, name)
		if _, statErr := os.Stat(existing); statErr == nil {
			return "", fmt.Errorf("%w: %s", ErrConfigExists, existing)
		}
	}

	if err = os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("config: cannot create %s: %w", dir, err)
	}

	path := filepath.Join(dir, defaultConfigName)
	if err = os.WriteFile(path, defaultConfig, 0o600); err != nil {
		return "", fmt.Errorf("config: cannot write %s: %w", path, err)
	}

	return path, nil
}

// Policies maps the parsed reviewer entries to reviewloop policies.
func Policies(cfg *Config) ([]reviewer.Policy, error) {
	policies := make([]reviewer.Policy, 0, len(cfg.Reviewers))
	for _, r := range cfg.Reviewers {
		goal, err := mapGoal(r.Goal)
		if err != nil {
			return nil, err
		}
		policies = append(policies, reviewer.Policy{
			Identity: reviewer.Identity{
				Type: reviewer.Type(r.Type),
				Name: r.Name,
			},
			Goal:       goal,
			MaxRallies: r.MaxRallies,
			Trigger:    r.Trigger,
		})
	}
	return policies, nil
}

// repoRootFrom walks up from start to the first directory that contains a .git
// entry (a directory for normal clones, a file for worktrees) and returns it.
func repoRootFrom(start string) (string, error) {
	dir := start
	for {
		if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("config: not inside a git repository")
		}
		dir = parent
	}
}

func mapGoal(g GoalConfig) (reviewer.Goal, error) {
	switch {
	case g.Approved:
		return reviewer.GoalApproved, nil
	case g.AllConversationsResolved:
		return reviewer.GoalAllConversationsResolved, nil
	default:
		return "", errors.New("config: goal has no valid flag set")
	}
}
