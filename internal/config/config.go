// Package config loads Shepherd's machine configuration: how the daemon listens, and the
// repo profiles that say how each repo works (base branch, branch model, gate, shared
// paths, autonomy).
//
// A missing file is not an error: every field has a default, and the defaults are
// cautious (a human merges, tags and deploys; agents plan first).
package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Config is the whole file.
type Config struct {
	Daemon   Daemon             `yaml:"daemon" json:"daemon"`
	Defaults Profile            `yaml:"defaults" json:"defaults"`
	Repos    map[string]Profile `yaml:"repos" json:"repos,omitempty"`
}

// Daemon holds the daemon's own settings.
type Daemon struct {
	// Listen is a loopback host:port. Port 0 picks a free port; clients find it in the
	// daemon's runtime file.
	Listen string `yaml:"listen" json:"listen"`
}

// Branch models.
const (
	// Trunk: work branches off the base branch and lands on it; releases are tags.
	Trunk = "trunk"
	// Promotion: work lands on the first branch and is promoted through the rest
	// (for example dev, then staging, then main).
	Promotion = "promotion"
)

// Who may take an action.
const (
	Human = "human"
	Agent = "agent"
)

// Profile says how a repo works. In Repos, an empty field inherits from Defaults.
type Profile struct {
	BaseBranch  string   `yaml:"base_branch,omitempty" json:"base_branch,omitempty"`
	BranchModel string   `yaml:"branch_model,omitempty" json:"branch_model,omitempty"`
	Promotion   []string `yaml:"promotion,omitempty" json:"promotion,omitempty"`
	// Gate is the command that must pass in a worktree before work counts as green.
	Gate string `yaml:"gate,omitempty" json:"gate,omitempty"`
	// SharedPaths are globs that more than one lane may want; touching one takes a lease.
	SharedPaths []string `yaml:"shared_paths,omitempty" json:"shared_paths,omitempty"`
	// WorktreeRoot is where lane worktrees go. Empty means <workspace>/<repo>-worktrees.
	WorktreeRoot string   `yaml:"worktree_root,omitempty" json:"worktree_root,omitempty"`
	Autonomy     Autonomy `yaml:"autonomy,omitempty" json:"autonomy"`
}

// Autonomy records what agents may do unasked in a repo.
type Autonomy struct {
	Merge  string `yaml:"merge,omitempty" json:"merge,omitempty"`
	Tag    string `yaml:"tag,omitempty" json:"tag,omitempty"`
	Deploy string `yaml:"deploy,omitempty" json:"deploy,omitempty"`
	// PlanFirst means an agent proposes a plan and waits before changing anything.
	PlanFirst *bool `yaml:"plan_first,omitempty" json:"plan_first,omitempty"`
}

// Default is the configuration with no file.
func Default() Config {
	yes := true
	return Config{
		Daemon: Daemon{Listen: "127.0.0.1:0"},
		Defaults: Profile{
			BaseBranch:  "main",
			BranchModel: Trunk,
			Autonomy:    Autonomy{Merge: Human, Tag: Human, Deploy: Human, PlanFirst: &yes},
		},
	}
}

// Load reads path over the defaults. A missing file gives Default().
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	c, err := Parse(b)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse reads YAML over the defaults and validates the result. Unknown keys are errors,
// so a typo cannot silently fall back to a default.
func Parse(b []byte) (Config, error) {
	c := Default()
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var file Config
	if err := dec.Decode(&file); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, err
	}
	if file.Daemon.Listen != "" {
		c.Daemon.Listen = file.Daemon.Listen
	}
	c.Defaults = merge(c.Defaults, file.Defaults)
	c.Repos = file.Repos
	return c, c.Validate()
}

// Validate checks every value against its closed set.
func (c Config) Validate() error {
	var errs []error
	host, _, err := net.SplitHostPort(c.Daemon.Listen)
	if err != nil {
		errs = append(errs, fmt.Errorf("daemon.listen: %w", err))
	} else if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		// Authentication is a local token today; logins come with the hosted service.
		errs = append(errs, fmt.Errorf("daemon.listen: %q is not a loopback address", c.Daemon.Listen))
	}
	errs = append(errs, c.Defaults.validate("defaults")...)
	for name := range c.Repos {
		errs = append(errs, c.Profile(name).validate("repos."+name)...)
	}
	return errors.Join(errs...)
}

// Profile returns the effective profile for a repo: its entry in Repos over Defaults.
func (c Config) Profile(repo string) Profile {
	return merge(c.Defaults, c.Repos[repo])
}

func (p Profile) validate(at string) []error {
	var errs []error
	if p.BaseBranch == "" {
		errs = append(errs, fmt.Errorf("%s.base_branch: empty", at))
	}
	switch p.BranchModel {
	case Trunk:
	case Promotion:
		if len(p.Promotion) < 2 {
			errs = append(errs, fmt.Errorf("%s.promotion: the promotion model needs at least two branches", at))
		}
	default:
		errs = append(errs, fmt.Errorf("%s.branch_model: %q is not %s or %s", at, p.BranchModel, Trunk, Promotion))
	}
	for field, v := range map[string]string{"merge": p.Autonomy.Merge, "tag": p.Autonomy.Tag, "deploy": p.Autonomy.Deploy} {
		if v != Human && v != Agent {
			errs = append(errs, fmt.Errorf("%s.autonomy.%s: %q is not %s or %s", at, field, v, Human, Agent))
		}
	}
	for _, g := range p.SharedPaths {
		if strings.TrimSpace(g) == "" {
			errs = append(errs, fmt.Errorf("%s.shared_paths: empty glob", at))
		}
	}
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return errs
}

// merge returns base with every set field of over applied.
func merge(base, over Profile) Profile {
	out := base
	if over.BaseBranch != "" {
		out.BaseBranch = over.BaseBranch
	}
	if over.BranchModel != "" {
		out.BranchModel = over.BranchModel
	}
	if over.Promotion != nil {
		out.Promotion = over.Promotion
	}
	if over.Gate != "" {
		out.Gate = over.Gate
	}
	if over.SharedPaths != nil {
		out.SharedPaths = over.SharedPaths
	}
	if over.WorktreeRoot != "" {
		out.WorktreeRoot = over.WorktreeRoot
	}
	if over.Autonomy.Merge != "" {
		out.Autonomy.Merge = over.Autonomy.Merge
	}
	if over.Autonomy.Tag != "" {
		out.Autonomy.Tag = over.Autonomy.Tag
	}
	if over.Autonomy.Deploy != "" {
		out.Autonomy.Deploy = over.Autonomy.Deploy
	}
	if over.Autonomy.PlanFirst != nil {
		out.Autonomy.PlanFirst = over.Autonomy.PlanFirst
	}
	return out
}

//go:embed template.yaml
var template []byte

// Template is the commented config file the daemon writes on first start.
func Template() []byte { return template }

// WriteTemplate writes Template to path if nothing is there yet, and reports whether it
// did. An existing file is never touched.
func WriteTemplate(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.Write(template); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}
