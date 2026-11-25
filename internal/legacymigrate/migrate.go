// Package legacymigrate converts the legacy release and deploy sections
// nerv moved into nerv-release's own files, as they were, into today's
// shape: the 5.0.0-alpha.1 ship: section, a release: section whose engine
// keys moved, the 4.2.0 release: and pipeline: sections, the bash deploy
// target (now script), and the project deploy providers
// (.ai-workflow/providers/deploy-<name>, now release-<name>). It is the
// port of nerv 6.6's internal/migrate release, ship, deploy and script
// conversions, written to release.yaml instead of nerv's config.yaml.
//
// Compute plans everything without writing; Apply writes the plan, each
// replaced file backed up first: a repository file under the repository's
// .nerv/backups/release-migrate-<timestamp>/ (git-ignored by .nerv/.gitignore),
// the global release.yaml next to it (atomicfile, "bak-migrate-"). A key
// already set in today's shape is never overwritten: it is kept and
// reported. A repository's personal settings are dropped and reported,
// never written to the global file. A second run plans nothing.
package legacymigrate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/war-apps/nerv-kit-by-gentle-ai/aiworkflow"
	"github.com/war-apps/nerv-kit-by-gentle-ai/atomicfile"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/config"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/deployconfig"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/refusal"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/releaseconfig"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/workflowconfig"
)

// BackupSuffix names the backup of the global release.yaml Apply replaces
// (atomicfile's "<path>.<suffix><timestamp>").
const BackupSuffix = "bak-migrate-"

// backupTimeFormat is the timestamp of a backup (atomicfile's).
const backupTimeFormat = "20060102-150405"

// ProjectProvidersDir is the repository's own providers folder, relative
// to its root.
const ProjectProvidersDir = ".ai-workflow/providers"

// releaseFile is the repository release.yaml, as the notes name it.
const releaseFile = ".ai-workflow/release.yaml"

// globalFile is the global release.yaml, as the notes name it.
const globalFile = "the global release.yaml"

// Options is one migration.
type Options struct {
	// RepoRoot is the repository's top level; "" migrates no repository.
	RepoRoot string
	// UserPath is the global release.yaml (--user); "" leaves it alone.
	// The repository's personal keys never go there: they are dropped
	// and reported either way.
	UserPath string
	// ExtensionsFS holds the provider manifests (release-ssh-compose's
	// user-scope keys).
	ExtensionsFS fs.FS
	// OriginURL is the repository's origin URL ("" when it has none).
	OriginURL string
}

// Kind is what a Change does.
type Kind int

const (
	// Write creates or replaces Path with New.
	Write Kind = iota
	// Rename moves Path (a file or a directory) to To.
	Rename
)

// Change is one file or directory the migration writes or moves.
type Change struct {
	Kind Kind
	// Path is absolute; To is a Rename's destination.
	Path, To string
	// Old is the current content (nil for a new file); New is a Write's.
	Old, New []byte
	// Why says, in a few words a user reads, why it changes.
	Why string
}

// Plan is everything one migration does.
type Plan struct {
	// RepoRoot is the repository migrated ("" for none).
	RepoRoot string
	// Changes are applied in order.
	Changes []Change
	// Notes say what was converted; Warnings what has no equivalent or
	// needs the user's hand.
	Notes, Warnings []string
}

// Empty reports whether the plan changes nothing.
func (p *Plan) Empty() bool { return len(p.Changes) == 0 }

func (p *Plan) note(format string, args ...any) {
	p.Notes = append(p.Notes, fmt.Sprintf(format, args...))
}

func (p *Plan) warn(format string, args ...any) {
	p.Warnings = append(p.Warnings, fmt.Sprintf(format, args...))
}

func refuse(format string, args ...any) error {
	return &refusal.Error{Err: fmt.Errorf(format, args...)}
}

// userState is the global release.yaml being built (nil without --user).
type userState struct {
	path     string
	doc      *config.Document
	original []byte
	exists   bool
}

// Compute plans the migration of o without writing anything.
func Compute(o Options) (*Plan, error) {
	p := &Plan{RepoRoot: o.RepoRoot}
	var user *userState
	if o.UserPath != "" {
		data, exists, err := readOptional(o.UserPath)
		if err != nil {
			return nil, err
		}
		user = &userState{path: o.UserPath, doc: config.Parse(string(data)), original: data, exists: exists}
		renameScriptTargets(p, user.doc, "deploy", globalFile+": deploy")
		renameScriptTargets(p, user.doc, workflowconfig.DefaultsKey, globalFile+": defaults.deploy")
	}
	var repo []Change
	if o.RepoRoot != "" {
		var err error
		if repo, err = planRepo(p, o); err != nil {
			return nil, err
		}
		// The backups of a repository file go under its .nerv/: refused
		// before anything is written when it is redirected.
		if len(repo) > 0 {
			if err := checkBackupDir(o.RepoRoot); err != nil {
				return nil, err
			}
		}
	}
	// The global file first: it is converted on its own content only.
	if user != nil {
		text := user.doc.String()
		if !user.exists {
			text = strings.TrimLeft(text, "\r\n")
		}
		if text != string(user.original) {
			c := Change{Kind: Write, Path: user.path, New: []byte(text), Why: "nerv-release's global configuration, converted"}
			if user.exists {
				c.Old = user.original
			}
			p.Changes = append(p.Changes, c)
		}
	}
	p.Changes = append(p.Changes, repo...)
	return p, nil
}

// planRepo plans the repository at o.RepoRoot: its release.yaml and its
// project deploy providers.
func planRepo(p *Plan, o Options) ([]Change, error) {
	root := o.RepoRoot
	if err := checkProjectDir(root); err != nil {
		return nil, err
	}
	var changes []Change
	moves, err := moveProjectDeployProviders(p, root)
	if err != nil {
		return nil, err
	}
	changes = append(changes, moves...)

	path := aiworkflow.ProjectConfigPath(root, workflowconfig.Tool)
	data, exists, err := readOptional(path)
	if err != nil || !exists {
		return changes, err
	}
	doc, err := convert(p, o, config.Parse(string(data)))
	if err != nil {
		return nil, err
	}
	text := doc.String()
	if text == string(data) {
		return changes, nil
	}
	if err := checkLoads(text); err != nil {
		return nil, err
	}
	return append(changes, Change{Kind: Write, Path: path, Old: data, New: []byte(text), Why: "converted to today's release: and deploy: sections"}), nil
}

// convert rewrites doc, the repository release.yaml, in place.
func convert(p *Plan, o Options, doc *config.Document) (*config.Document, error) {
	var dep *deployTranslation
	if doc.KeyExists("pipeline") {
		var err error
		if dep, err = translatePipeline(p, []byte(doc.String()), o.ExtensionsFS); err != nil {
			return nil, err
		}
		doc.SetBlock("pipeline", "")
		p.note("pipeline: removed (nerv pipeline was removed)")
	}
	githubRelease := false
	switch {
	case doc.KeyExists(releaseconfig.AlphaSectionKey):
		// The 5.0.0-alpha.1 ship: section is today's release: under its
		// old name, with the keys of before the version and build slots.
		if err := migrateShip(p, doc); err != nil {
			return nil, err
		}
	case len(releaseconfig.LegacyKeys([]byte(doc.String()))) > 0:
		// A 4.2.0 release: section has today's key: its 4.2.0 keys tell
		// it apart.
		p.note("%s: the release: section is the 4.2.0 one (%s)", releaseFile, strings.Join(releaseconfig.LegacyKeys([]byte(doc.String())), ", "))
		var err error
		if doc, githubRelease, err = migrateRelease(p, o.RepoRoot, doc); err != nil {
			return nil, err
		}
	case doc.KeyExists(releaseconfig.SectionKey):
		if err := moveReleaseKeys(p, doc); err != nil {
			return nil, err
		}
	}
	if dep != nil || githubRelease {
		if err := writeDeploy(p, o, doc, dep); err != nil {
			return nil, err
		}
	}
	renameScriptTargets(p, doc, "deploy", releaseFile)
	return doc, nil
}

// writeDeploy writes the deploy: section a 4.2.0 configuration implied:
// the pipeline's targets, and the git host when origin is GitHub. A
// deploy: section already there is kept, and what was not written is
// reported.
func writeDeploy(p *Plan, o Options, doc *config.Document, dep *deployTranslation) error {
	if dep == nil {
		dep = &deployTranslation{}
	}
	if doc.KeyExists("deploy") {
		var names []string
		for _, t := range dep.targets {
			names = append(names, t.name)
		}
		if len(names) > 0 {
			p.warn("deploy: already set, kept: the release-ssh-compose targets translated from pipeline: (%s) were not written; add them by hand", strings.Join(names, ", "))
		}
		return nil
	}
	switch {
	case isGitHub(o.OriginURL):
		dep.host = githubHost
	case o.OriginURL == "":
		p.warn("deploy.host: the repository has no origin, so no git host is written: set deploy.host when it has one")
	default:
		p.warn("deploy.host: origin %s is not GitHub and nerv-release ships no other git host provider yet: no tag release is created on the host", o.OriginURL)
	}
	block, err := deployBlock(dep)
	if err != nil {
		return err
	}
	doc.SetBlock("deploy", block)
	p.note("deploy: written (git writes, the tag and the host release are nerv-release deploy's)")
	return nil
}

// checkProjectDir refuses a .ai-workflow folder that is a symlink or
// resolves outside the repository: a cloned repository must never
// redirect what migrate reads and writes.
func checkProjectDir(root string) error {
	dir := aiworkflow.ProjectDir(root)
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return refuse("refusing %s: a cloned repository must not redirect its .ai-workflow directory through a symlink", dir)
	}
	if !info.IsDir() {
		return refuse("refusing %s: expected a directory", dir)
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return refuse("refusing %s: it resolves outside the repository (%s)", dir, resolvedDir)
	}
	return nil
}

// readOptional reads path; a missing file is (nil, false, nil), and a
// symlink or any other non-regular file is refused.
func readOptional(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, refuse("refusing %s: it is not a regular file (a symlink?); migrate it by hand", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// checkLoads refuses a migrated release.yaml that today's strict loaders
// of the release: and deploy: sections reject.
func checkLoads(text string) error {
	dir, err := os.MkdirTemp("", "nerv-release-migrate-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := aiworkflow.ProjectConfigPath(dir, workflowconfig.Tool)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return err
	}
	clean := func(err error) string { return strings.ReplaceAll(err.Error(), dir+string(filepath.Separator), "") }
	if _, err := releaseconfig.Load(dir); err != nil && !errors.Is(err, releaseconfig.ErrNotConfigured) {
		return refuse("the migrated %s does not load: %s; nothing was written", releaseFile, clean(err))
	}
	if _, err := deployconfig.Load(dir); err != nil && !errors.Is(err, deployconfig.ErrNotConfigured) {
		return refuse("the migrated %s does not load: %s; nothing was written", releaseFile, clean(err))
	}
	return nil
}

// Apply writes the plan in order: each replaced file is backed up
// first, a repository file under <repository>/.nerv/backups/
// migrate-<timestamp>/<its repository-relative path> (.nerv/ gets a
// .gitignore of "*" when it has none, so no backup is ever committed),
// the global release.yaml next to it. It returns the backups. A failure
// stops at that change: the ones before it are applied (each one whole),
// and a new run picks up what is left.
func Apply(p *Plan, now time.Time) ([]string, error) {
	var backups []string
	for _, c := range p.Changes {
		switch c.Kind {
		case Rename:
			if _, err := os.Lstat(c.To); err == nil {
				return backups, refuse("%s already exists; nothing more was moved", c.To)
			}
			if err := os.MkdirAll(filepath.Dir(c.To), 0o755); err != nil {
				return backups, err
			}
			if err := os.Rename(c.Path, c.To); err != nil {
				return backups, err
			}
		case Write:
			rel, inRepo := repoRelative(p.RepoRoot, c.Path)
			if c.Old != nil && !inRepo {
				backup, err := atomicfile.Save(c.Path, c.New, atomicfile.Options{Now: now, BackupSuffix: BackupSuffix})
				if err != nil {
					return backups, err
				}
				if backup != "" {
					backups = append(backups, backup)
				}
				continue
			}
			if c.Old != nil {
				backup, err := backupRepoFile(p.RepoRoot, rel, c.Old, now)
				if err != nil {
					return backups, err
				}
				backups = append(backups, backup)
			}
			if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
				return backups, err
			}
			if err := atomicfile.Write(c.Path, c.New, 0o644); err != nil {
				return backups, err
			}
		}
	}
	return backups, nil
}

// repoRelative is path relative to the repository root, and whether it is
// inside it.
func repoRelative(root, path string) (string, bool) {
	if root == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// backupDir is the repository's .nerv/backups folder.
func backupDir(root string) string { return filepath.Join(root, ".nerv", "backups") }

// checkBackupDir refuses a .nerv or .nerv/backups that is a symlink or not
// a directory: a cloned repository must never redirect where migrate
// writes its backups.
func checkBackupDir(root string) error {
	for _, dir := range []string{filepath.Join(root, ".nerv"), backupDir(root)} {
		info, err := os.Lstat(dir)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return refuse("refusing %s: expected a directory, not a symlink (migrate keeps its backups there); a cloned repository must not redirect it", dir)
		}
	}
	return nil
}

// backupRepoFile writes old, a repository file's content, to
// .nerv/backups/release-migrate-<now>/<rel>, never over an existing file, after
// making sure .nerv/ ignores itself. It returns the backup's path.
func backupRepoFile(root, rel string, old []byte, now time.Time) (string, error) {
	if err := checkBackupDir(root); err != nil {
		return "", err
	}
	if err := ensureNervIgnored(root); err != nil {
		return "", err
	}
	// nerv migrate backs its own files up under .nerv/backups/migrate-<time>/
	// just before it runs this command: release-migrate-<time> keeps the
	// two apart, and a numbered sibling takes a folder that already holds
	// rel (two runs in the same second).
	base := filepath.Join(backupDir(root), "release-migrate-"+now.Format(backupTimeFormat))
	var f *os.File
	var path string
	for n := 1; ; n++ {
		dir := base
		if n > 1 {
			dir = fmt.Sprintf("%s-%d", base, n)
		}
		path = filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		var err error
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || n >= 100 {
			return "", err
		}
	}
	if _, err := f.Write(old); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}

// ensureNervIgnored writes <root>/.nerv/.gitignore of "*" when it has
// none, so nothing under .nerv/ is committed and the repository's own
// .gitignore is never touched (as internal/artifact does for the
// artifacts).
func ensureNervIgnored(root string) error {
	ignore := filepath.Join(root, ".nerv", ".gitignore")
	if _, err := os.Lstat(ignore); !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ignore), 0o755); err != nil {
		return err
	}
	return os.WriteFile(ignore, []byte("*\n"), 0o644)
}

// Render is the plan as a user reads it: the numbered steps in the order
// Apply takes them (paths
// relative to the repository, absolute for the global file), how the
// configuration changes, and what needs the user's hand.
func Render(p *Plan) string {
	var b strings.Builder
	rel := func(path string) string {
		if p.RepoRoot != "" {
			if r, err := filepath.Rel(p.RepoRoot, path); err == nil && !strings.HasPrefix(r, "..") {
				return filepath.ToSlash(r)
			}
		}
		return path
	}
	for i, c := range p.Changes {
		var s string
		switch c.Kind {
		case Write:
			verb := "Update"
			if c.Old == nil {
				verb = "Create"
			}
			s = verb + " " + rel(c.Path)
		case Rename:
			s = "Move " + rel(c.Path) + " to " + rel(c.To)
		}
		if c.Why != "" {
			s += ": " + c.Why
		}
		fmt.Fprintf(&b, "  %d. %s\n", i+1, s)
	}
	list := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s:\n", title)
		for _, it := range items {
			fmt.Fprintf(&b, "  - %s\n", it)
		}
	}
	list("How the configuration changes", p.Notes)
	list("Check by hand", p.Warnings)
	return b.String()
}
