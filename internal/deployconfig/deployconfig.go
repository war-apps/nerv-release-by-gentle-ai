// Package deployconfig reads the deploy: section of a repository's
// .ai-workflow/release.yaml, the configuration of `nerv-release deploy`:
//
//	deploy:
//	  host: github                 # the git host provider (release-<name>)
//	  publish: true                # push the artifact's images (default true)
//	  targets:                     # optional; deployed in this order
//	    - name: staging
//	      provider: ssh-compose    # a target provider (release-<name>)
//	      config: { environment: staging }
//	  git:
//	    commit_message: "chore(release): ${nextRelease.version} [skip ci]\n\n${nextRelease.notes}"
//	    remote: origin
//
// Like internal/releaseconfig it reads the top-level block through
// internal/workflowconfig (the config layer the two share) and parses it
// strictly: unknown keys are refused. Which config keys a target accepts
// is its provider manifest's business (U4's internal/deployrun).
package deployconfig

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"

	nervrelease "github.com/war-apps/nerv-release-by-gentle-ai"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/config"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseconfig"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseconfig/yamlsection"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/workflowconfig"
)

// ErrNotConfigured is returned by Load when the repository has no
// .ai-workflow/release.yaml or the file has no deploy: key.
var ErrNotConfigured = errors.New("deploy: not configured in .ai-workflow/release.yaml")

// DefaultCommitMessage is @semantic-release/git's default release commit
// message.
const DefaultCommitMessage = "chore(release): ${nextRelease.version} [skip ci]\n\n${nextRelease.notes}"

// DefaultRemote is the remote deploy clones and pushes to.
const DefaultRemote = "origin"

// Deploy is the deploy: section.
type Deploy struct {
	// Host is the git host provider's name; empty creates no release.
	Host string `yaml:"host"`
	// Publish makes nerv-release deploy push the artifact's images itself,
	// in its publish step; nil means true (Publishes).
	Publish *bool `yaml:"publish"`
	// Targets are deployed in order; none releases without deploying.
	Targets []Target `yaml:"targets"`
	Git     Git      `yaml:"git"`
}

// Publishes reports whether nerv-release deploy pushes the artifact's
// images in its own publish step (deploy.publish, default true).
func (d *Deploy) Publishes() bool { return d.Publish == nil || *d.Publish }

// Target is one deploy target.
type Target struct {
	// Name identifies the target (--target). Required, unique.
	Name string `yaml:"name"`
	// Provider is the target provider's name. Required.
	Provider string `yaml:"provider"`
	// Config are the provider's config keys for this target.
	Config map[string]string `yaml:"config"`
}

// Git configures the release commit and the push.
type Git struct {
	// CommitMessage is the release commit's message, with the placeholders
	// of Placeholders. Default: DefaultCommitMessage.
	CommitMessage string `yaml:"commit_message"`
	// Remote is the remote cloned and pushed to. Default: DefaultRemote.
	Remote string `yaml:"remote"`
}

// Release is what a commit message is rendered with.
type Release struct {
	Version, Tag, Channel, Notes string
}

// Placeholders are the commit message placeholders: @semantic-release/git's
// lodash ones and their short forms.
var Placeholders = []string{
	"${nextRelease.version}", "${nextRelease.gitTag}", "${nextRelease.channel}", "${nextRelease.notes}",
	"${version}", "${tag}", "${channel}", "${notes}",
}

var (
	nameRe        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	providerRe    = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	keyRe         = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	remoteRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	placeholderRe = regexp.MustCompile(`\$\{[^}]*\}`)
)

// CommitMessage renders message for r in one pass, so the notes are never
// expanded themselves.
func CommitMessage(message string, r Release) string {
	values := map[string]string{
		"${nextRelease.version}": r.Version, "${nextRelease.gitTag}": r.Tag, "${nextRelease.channel}": r.Channel, "${nextRelease.notes}": r.Notes,
		"${version}": r.Version, "${tag}": r.Tag, "${channel}": r.Channel, "${notes}": r.Notes,
	}
	return placeholderRe.ReplaceAllStringFunc(message, func(p string) string {
		if v, ok := values[p]; ok {
			return v
		}
		return p
	})
}

// Load reads the deploy: section of repoRoot's .ai-workflow/release.yaml
// through internal/workflowconfig, the config layer shared with
// releaseconfig. A project setting a personal (scope: user) key, such as
// release-ssh-compose's ssh_known_hosts or infisical_domain, is refused
// (R-S4): a deploy must not run with an ambiguous trust boundary between
// the repository and the user's own configuration. This applies both to
// the section-level deploy.providers.<provider>.<key> shape
// (refusePersonalKeys) and to the same key reaching a target through its
// own config: override, deploy.targets[].config.<key>
// (refuseTargetPersonalKeys): a target's provider still reads personal
// keys from the user file only (internal/deployrun), so a repository
// value there would be silently ignored at run time while looking, to
// anyone reading the file, like the value in effect.
func Load(repoRoot string) (*Deploy, error) {
	cfg, err := workflowconfig.Load("", "", repoRoot)
	if err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}
	if !cfg.ProjectExists {
		return nil, ErrNotConfigured
	}
	personal, err := workflowconfig.LoadPersonal(nervrelease.ExtensionsFS())
	if err != nil {
		return nil, err
	}
	if err := refusePersonalKeys(cfg.Project, personal); err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.ProjectPath, err)
	}
	data := []byte(cfg.Project.String())
	d, err := parse(data)
	if err != nil && !errors.Is(err, ErrNotConfigured) {
		return nil, fmt.Errorf("%s: %w", cfg.ProjectPath, err)
	}
	if err != nil {
		return nil, err
	}
	if err := refuseTargetPersonalKeys(d, personal); err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.ProjectPath, err)
	}
	return d, nil
}

// refusePersonalKeys reports every personal key (workflowconfig.Personal)
// the project document sets, such as release-ssh-compose's
// ssh_known_hosts, as one error naming each. It scans the whole document
// as YAML (unlike parse's tolerant per-block cut), so a scan error is not
// reported here: it falls through to parse, which reports whatever is
// actually wrong with the deploy: block.
func refusePersonalKeys(project *config.Document, personal workflowconfig.Personal) error {
	keys, err := workflowconfig.ProjectPersonalKeys(project, personal)
	if err != nil || len(keys) == 0 {
		return nil
	}
	msgs := make([]string, len(keys))
	for i, k := range keys {
		msgs[i] = workflowconfig.PersonalKeyMessage(k)
	}
	return errors.New(strings.Join(msgs, "; "))
}

// refuseTargetPersonalKeys reports every key of a target's own config:
// override that is personal for that target's provider
// (deploy.providers.<provider>.<key>, the same namespace
// workflowconfig.Personal indexes), named by where it was actually
// written (deploy.targets[i].config.<key>), as one error naming each.
func refuseTargetPersonalKeys(d *Deploy, personal workflowconfig.Personal) error {
	var msgs []string
	for i, t := range d.Targets {
		for _, key := range slices.Sorted(maps.Keys(t.Config)) {
			if personal.Has("deploy.providers." + t.Provider + "." + key) {
				msgs = append(msgs, workflowconfig.PersonalKeyMessage(fmt.Sprintf("deploy.targets[%d].config.%s", i, key)))
			}
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return errors.New(strings.Join(msgs, "; "))
}

func parse(data []byte) (*Deploy, error) {
	if msg := releaseconfig.RemovedPipeline(data); msg != "" {
		return nil, errors.New(msg)
	}
	node, err := yamlsection.Locate(data, "deploy")
	var problem *yamlsection.ProblemError
	switch {
	case errors.Is(err, yamlsection.ErrAbsent):
		return nil, ErrNotConfigured
	case errors.As(err, &problem):
		return nil, errors.New(problem.Problem)
	case err != nil:
		return nil, err
	}
	var d Deploy
	if node.ShortTag() != "!!null" {
		unknown, err := yamlsection.UnknownFields(node, reflect.TypeFor[Deploy](), "deploy")
		if err != nil {
			return nil, err
		}
		if len(unknown) > 0 {
			return nil, errors.New(strings.Join(unknown, "; "))
		}
		if err := node.Decode(&d); err != nil {
			return nil, errors.New(strings.Join(yamlsection.DecodeProblems("deploy", err), "; "))
		}
	}
	if d.Git.CommitMessage == "" {
		d.Git.CommitMessage = DefaultCommitMessage
	}
	if d.Git.Remote == "" {
		d.Git.Remote = DefaultRemote
	}
	if problems := d.validate(); len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return &d, nil
}

func (d *Deploy) validate() []string {
	var p []string
	add := func(format string, args ...any) { p = append(p, fmt.Sprintf(format, args...)) }
	if d.Host != "" && !providerRe.MatchString(d.Host) {
		add("deploy.host %q must be a provider name (lower-case letters, digits and dashes)", d.Host)
	}
	seen := map[string]int{}
	for i, t := range d.Targets {
		at := fmt.Sprintf("deploy.targets[%d]", i)
		if !nameRe.MatchString(t.Name) {
			add("%s.name %q must be lower-case letters, digits, ., _ and -", at, t.Name)
		} else if j, dup := seen[t.Name]; dup {
			add("%s.name: %q is also targets[%d]", at, t.Name, j)
		} else {
			seen[t.Name] = i
		}
		switch {
		case t.Provider == "":
			add("%s.provider: required", at)
		case !providerRe.MatchString(t.Provider):
			add("%s.provider %q must be a provider name", at, t.Provider)
		}
		for key := range t.Config {
			if !keyRe.MatchString(key) {
				add("%s.config.%s: a key is lower-case letters, digits and underscores", at, key)
			}
		}
	}
	for _, ph := range placeholderRe.FindAllString(d.Git.CommitMessage, -1) {
		known := false
		for _, k := range Placeholders {
			known = known || k == ph
		}
		if !known {
			add("deploy.git.commit_message: unknown placeholder %s (known: %s)", ph, strings.Join(Placeholders, ", "))
		}
	}
	if !remoteRe.MatchString(d.Git.Remote) {
		add("deploy.git.remote %q must be a remote name", d.Git.Remote)
	}
	return p
}
