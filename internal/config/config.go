// Package config loads, parses, and resolves the github-review-loop config file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	z "github.com/Oudwins/zog"
	"gopkg.in/yaml.v3"

	"github.com/sushichan044/github-review-loop/internal/reviewloop"
	"github.com/sushichan044/github-review-loop/internal/xdg"
)

const configFileName = "config.yaml"
const appDirName = "github-review-loop"
const defaultMaxRallies = 5
const scopeRepo = "repo"

// GoalConfig holds the two mutually exclusive goal flags as parsed from YAML.
type GoalConfig struct {
	Approved                 bool `zog:"approved"`
	AllConversationsResolved bool `zog:"all-conversations-resolved"`
}

// ReviewerConfig is a single reviewer entry inside a loop.
type ReviewerConfig struct {
	Type       string     `zog:"type"`
	Name       string     `zog:"name"`
	Goal       GoalConfig `zog:"goal"`
	MaxRallies int        `zog:"max-rallies"`
	Trigger    string     `zog:"trigger"`
}

// LoopEntry is one element of the top-level `loops` array.
type LoopEntry struct {
	Scope     string           `zog:"scope"`
	Owner     string           `zog:"owner"`
	Repo      string           `zog:"repo"`
	Reviewers []ReviewerConfig `zog:"reviewers"`
}

// Config is the parsed, validated representation of config.yaml.
type Config struct {
	Loops []LoopEntry `zog:"loops"`
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

	loopEntrySchema := z.Struct(z.Shape{
		"Scope":     z.String().OneOf([]string{"owner", scopeRepo}),
		"Owner":     z.String().Required(),
		"Repo":      z.String(),
		"Reviewers": z.Slice(reviewerSchema),
	}).TestFunc(func(dataPtr any, ctx z.Ctx) bool {
		e, ok := dataPtr.(*LoopEntry)
		if !ok {
			return false
		}
		if e.Scope == scopeRepo && e.Repo == "" {
			ctx.AddIssue(ctx.Issue().SetMessage("'repo' is required when scope is 'repo'"))
			return false
		}
		return true
	})

	return z.Struct(z.Shape{
		"Loops": z.Slice(loopEntrySchema),
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

// Load reads and parses the config file from the XDG config home.
// Returns an error wrapping [os.ErrNotExist] if the file does not exist.
func Load() (*Config, error) {
	home, err := xdg.ConfigHome()
	if err != nil {
		return nil, fmt.Errorf("config: cannot determine config home: %w", err)
	}

	path := filepath.Join(home, appDirName, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config: file not found at %s: %w", path, os.ErrNotExist)
		}
		return nil, fmt.Errorf("config: cannot read %s: %w", path, err)
	}

	return Parse(data)
}

// Resolve merges owner-scope and repo-scope reviewer lists for the given owner/repo
// and returns one Policy per merged reviewer. Repo-scope overrides owner-scope on
// the same identity (type + name, case-insensitive).
func Resolve(cfg *Config, owner, repo string) ([]reviewloop.Policy, error) {
	var ownerReviewers []ReviewerConfig
	var repoReviewers []ReviewerConfig

	for _, loop := range cfg.Loops {
		if !strings.EqualFold(loop.Owner, owner) {
			continue
		}
		switch loop.Scope {
		case "owner":
			ownerReviewers = append(ownerReviewers, loop.Reviewers...)
		case scopeRepo:
			if strings.EqualFold(loop.Repo, repo) {
				repoReviewers = append(repoReviewers, loop.Reviewers...)
			}
		}
	}

	type identityKey struct {
		typ  string
		name string
	}
	key := func(r ReviewerConfig) identityKey {
		return identityKey{typ: r.Type, name: strings.ToLower(r.Name)}
	}

	repoByIdentity := make(map[identityKey]ReviewerConfig, len(repoReviewers))
	for _, r := range repoReviewers {
		repoByIdentity[key(r)] = r
	}

	var merged []ReviewerConfig
	ownerKeys := make(map[identityKey]struct{}, len(ownerReviewers))
	for _, r := range ownerReviewers {
		k := key(r)
		ownerKeys[k] = struct{}{}
		if override, ok := repoByIdentity[k]; ok {
			merged = append(merged, override)
		} else {
			merged = append(merged, r)
		}
	}
	for _, r := range repoReviewers {
		if _, seen := ownerKeys[key(r)]; !seen {
			merged = append(merged, r)
		}
	}

	policies := make([]reviewloop.Policy, 0, len(merged))
	for _, r := range merged {
		goal, err := mapGoal(r.Goal)
		if err != nil {
			return nil, err
		}
		policies = append(policies, reviewloop.Policy{
			Identity: reviewloop.ReviewerIdentity{
				Type: reviewloop.ReviewerType(r.Type),
				Name: r.Name,
			},
			Goal:       goal,
			MaxRallies: r.MaxRallies,
			Trigger:    r.Trigger,
		})
	}
	return policies, nil
}

func mapGoal(g GoalConfig) (reviewloop.Goal, error) {
	switch {
	case g.Approved:
		return reviewloop.GoalApproved, nil
	case g.AllConversationsResolved:
		return reviewloop.GoalAllConversationsResolved, nil
	default:
		return "", errors.New("config: goal has no valid flag set")
	}
}
