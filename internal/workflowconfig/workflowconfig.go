// Package workflowconfig is nerv-release's one loader of its two
// configuration files: the user-scope global release.yaml
// ("<aiworkflow.UserConfigDir>/ai-workflow/release.yaml") and the
// project-scope release.yaml ("<repo>/.ai-workflow/release.yaml"). Each
// holds the same two top-level sections, release: and deploy:, exactly as
// the project file does (R-S4). Values merge key by key, with the project
// winning, except each provider config key a manifest declares scope:
// user (such as release-ssh-compose's ssh_auth), which is read from the
// user file instead (scope.go). The user file's defaults: section
// (defaults.release, defaults.deploy) never applies at run time — it only
// seeds a new project file (Seed).
//
// Ported from nerv's internal/workflowconfig, trimmed: nerv-release has
// only the two sections release and deploy (nerv's tasks section and its
// PersonalTaskKeys do not apply here), one extension (release) instead of
// several, and no legacy file format to refuse (R-S1 drops
// RefusePreviousProject; unlike nerv there is no previous format).
package workflowconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/war-apps/nerv-kit-by-gentle-ai/aiworkflow"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/config"
)

// Tool is this tool's name in the kit's user/project configuration
// layout (aiworkflow): the project file is
// "<repoRoot>/.ai-workflow/release.yaml", the user file
// "<aiworkflow.UserConfigDir>/ai-workflow/release.yaml".
const Tool = "release"

// Config is the two release.yaml documents of one home and repository.
type Config struct {
	// UserPath and ProjectPath are the files read; each is empty when
	// Load was given no home (UserPath) or no repository (ProjectPath).
	UserPath, ProjectPath string
	// UserExists and ProjectExists report whether each file exists.
	UserExists, ProjectExists bool
	// User and Project are the parsed documents (empty when missing or
	// not requested).
	User, Project *config.Document
	// Personal are the keys under Sections that Scalar still reads from
	// the user file (LoadPersonal); the zero value has none.
	Personal Personal
}

// Load reads the project file of repoRoot (when not empty) and, when
// home is not empty, the user file of home on goos
// (aiworkflow.UserConfigPath). A missing file is not an error.
func Load(home, goos, repoRoot string) (Config, error) {
	cfg := Config{Project: config.Parse("")}
	if home != "" {
		cfg.UserPath = aiworkflow.UserConfigPath(home, goos, Tool)
		var err error
		if cfg.User, cfg.UserExists, err = loadDocument(cfg.UserPath); err != nil {
			return Config{}, fmt.Errorf("reading %s: %w", cfg.UserPath, err)
		}
	}
	if repoRoot == "" {
		return cfg, nil
	}
	cfg.ProjectPath = aiworkflow.ProjectConfigPath(repoRoot, Tool)
	var err error
	if cfg.Project, cfg.ProjectExists, err = loadDocument(cfg.ProjectPath); err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", cfg.ProjectPath, err)
	}
	return cfg, nil
}

// loadDocument reads path and wraps its content as a *config.Document. A
// missing file is reported as exists=false with an empty Document, not
// an error.
func loadDocument(path string) (*config.Document, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return config.Parse(""), false, nil
		}
		return nil, false, err
	}
	return config.Parse(string(data)), true, nil
}

// Scalar resolves a dotted key path, project first, then user. A key
// under one of Sections (release, deploy) is read from the project only,
// unless Personal has it; the user's defaults: section never applies.
func (c Config) Scalar(path string) (string, bool) {
	if c.Project != nil {
		if v, found := c.Project.Scalar(path); found {
			return v, true
		}
	}
	if c.User != nil && (!ProjectOnly(path) || c.Personal.Has(path)) {
		return c.User.Scalar(path)
	}
	return "", false
}
