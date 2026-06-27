// Package config loads, parses, and resolves the mergeable-please config file.
//
// Config lives at the repository root as .mergeable-please.yml (or .yaml).
// Committing it is how reviewer-loop policies are shared with collaborators and CI.
//
// The config file is optional: Load returns a fully-defaulted Config when no
// file is found, so `mergeable-please check` works out of the box with no setup.
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
	defaultConfigName = ".mergeable-please.yml"
	defaultMaxRallies = 5
)

// configNames are the accepted config file names, tried in order.
func configNames() []string {
	return []string{defaultConfigName, ".mergeable-please.yaml"}
}

// defaultConfigTemplate is the commented template written by Init.
//
//go:embed templates/mergeable-please.yml
var defaultConfigTemplate []byte

// ErrConfigExists is returned by Init when a config file already exists, so a
// committed policy is never overwritten.
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

// GitConfig holds git-layer settings.
type GitConfig struct {
	Remote            string `zog:"remote"`
	ConflictsResolved bool   `zog:"conflicts-resolved"`
}

// GitHubConfig holds GitHub-specific settings.
type GitHubConfig struct {
	Rulesets  bool             `zog:"rulesets"`
	Reviewers []ReviewerConfig `zog:"reviewers"`
}

// Config is the parsed, validated representation of .mergeable-please.yml.
type Config struct {
	Git    GitConfig    `zog:"git"`
	GitHub GitHubConfig `zog:"github"`
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

	gitSchema := z.Struct(z.Shape{
		"Remote":            z.String().Default("origin"),
		"ConflictsResolved": z.Bool().Default(true),
	})

	githubSchema := z.Struct(z.Shape{
		"Rulesets":  z.Bool().Default(true),
		"Reviewers": z.Slice(reviewerSchema),
	})

	return z.Struct(z.Shape{
		"Git":    gitSchema,
		"GitHub": githubSchema,
	})
}

// defaultConfig returns a Config with all defaults applied.
func defaultConfig() *Config {
	return &Config{
		Git: GitConfig{
			Remote:            "origin",
			ConflictsResolved: true,
		},
		GitHub: GitHubConfig{
			Rulesets:  true,
			Reviewers: []ReviewerConfig{},
		},
	}
}

// Parse unmarshals YAML data and validates it with zog.
// The input must use the git:/github: schema.
func Parse(data []byte) (*Config, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("config: yaml parse error: %w", err)
	}

	// Treat an empty/nil document as defaults.
	if raw == nil {
		return defaultConfig(), nil
	}

	// Seed cfg with defaults so that omitting a top-level section (e.g. "git:")
	// while providing another (e.g. "github:") still applies nested defaults for
	// the missing section. Without this, zog would leave nested fields at their
	// zero values instead of applying schema defaults.
	cfg := *defaultConfig()
	if errs := buildSchema().Parse(raw, &cfg); errs != nil {
		return nil, fmt.Errorf("config: validation error: %v", errs)
	}
	return &cfg, nil
}

// Load finds the config for the current repository and parses it.
// It walks up from the working directory to the repository root, then reads
// .mergeable-please.yml (or .yaml) at the root.
//
// When no config file is found, Load returns a fully-defaulted Config (no error).
// This allows `mergeable-please check` to work out of the box without any setup.
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
		// Not inside a git repo: return defaults rather than failing.
		// Commands that need git context (PR resolution) will fail on their own.
		return defaultConfig(), nil //nolint:nilerr // intentional: no repo → use defaults; caller will fail on git ops
	}

	for _, name := range configNames() {
		path := filepath.Join(root, name)
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			return Parse(data)
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return nil, fmt.Errorf("config: cannot read %s: %w", path, readErr)
		}
	}

	// No file found — return defaults.
	return defaultConfig(), nil
}

// Init writes the default config template to .mergeable-please.yml at the
// current repository root. It returns the path written.
// It returns ErrConfigExists if a config file already exists.
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

	for _, name := range configNames() {
		existing := filepath.Join(root, name)
		if _, statErr := os.Stat(existing); statErr == nil {
			return "", fmt.Errorf("%w: %s", ErrConfigExists, existing)
		}
	}

	path := filepath.Join(root, defaultConfigName)
	// G306: 0644 is intentional — this is a user-editable project config file, not a secrets file.
	if err = os.WriteFile(path, defaultConfigTemplate, 0o644); err != nil { //nolint:gosec // G306: user-editable config
		return "", fmt.Errorf("config: cannot write %s: %w", path, err)
	}

	return path, nil
}

// Policies maps the parsed reviewer entries to reviewer policies.
func Policies(cfg *Config) ([]reviewer.Policy, error) {
	policies := make([]reviewer.Policy, 0, len(cfg.GitHub.Reviewers))
	for _, r := range cfg.GitHub.Reviewers {
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
// entry and returns it.
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
		// Defensive dead code: zog validation rejects configs with no goal set.
		return "", errors.New("config: goal has no valid flag set")
	}
}
