// Package initrepo implements `nerv-release init`: writing this
// repository's own .ai-workflow/release.yaml, seeded from the global
// release.yaml's defaults: block, with an optional import of nerv's own
// .ai-workflow/config.yaml release:/deploy: blocks. Mirrors nerv-task's
// own internal/initrepo: a repository's own file is untrusted input (it
// travels with a clone), so a personal (scope: user) key is never
// written anywhere -- not the new repository file, and never the
// shared global file -- only reported by name as skipped (the same
// security rule as nerv-task init, R-S4).
package initrepo

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/war-apps/nerv-kit-by-gentle-ai/aiworkflow"
	"github.com/war-apps/nerv-kit-by-gentle-ai/atomicfile"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/config"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/workflowconfig"
)

// Deps bundles what Init needs.
type Deps struct {
	RepoRoot     string
	GlobalPath   string
	ExtensionsFS fs.FS
	Now          func() time.Time
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Options are nerv-release init's own flags.
type Options struct {
	// None skips writing the repository file entirely: release
	// configuration stays off in this repository.
	None bool
	// Import copies nerv's own .ai-workflow/config.yaml release:/deploy:
	// blocks into the new repository file, repo-scope keys only.
	Import bool
}

// Result reports what Init did.
type Result struct {
	Skipped            bool
	Path               string
	ImportedFrom       string
	SeededFromDefaults bool
	// SkippedPersonalKeys lists, as ready-to-print messages, every
	// scope: user key the import (or, in principle, the seed) carried,
	// that was deliberately never written anywhere.
	SkippedPersonalKeys []string
}

// nervConfigFile is nerv's own repository configuration file's name
// (the one --import reads).
const nervConfigFile = "config.yaml"

// Init writes this repository's own release.yaml.
func Init(deps Deps, opts Options) (Result, error) {
	if opts.None {
		return Result{Skipped: true}, nil
	}

	personal, err := workflowconfig.LoadPersonal(deps.ExtensionsFS)
	if err != nil {
		return Result{}, err
	}

	userDoc, err := loadOptionalDocument(deps.GlobalPath)
	if err != nil {
		return Result{}, err
	}

	project := config.Parse("")
	var result Result
	for _, section := range workflowconfig.Sections {
		if block, ok := workflowconfig.Seed(userDoc, section, personal); ok {
			project.SetBlock(section, strings.TrimRight(block, "\n"))
			result.SeededFromDefaults = true
		}
	}

	if opts.Import {
		nervConfigPath := filepath.Join(aiworkflow.ProjectDir(deps.RepoRoot), nervConfigFile)
		nervDoc, found, err := loadNervConfig(nervConfigPath)
		if err != nil {
			return Result{}, err
		}
		if found {
			var skipped []string
			anyImported := false
			for _, section := range workflowconfig.Sections {
				block, ok := nervDoc.Block(section)
				if !ok {
					continue
				}
				sectionDoc := config.Parse(strings.TrimRight(block, "\n") + "\n")
				keys, err := workflowconfig.PersonalKeysOf(sectionDoc, section, personal)
				if err != nil {
					return Result{}, err
				}
				for _, k := range keys {
					skipped = append(skipped, workflowconfig.PersonalKeyMessage(k))
				}
				if err := workflowconfig.Strip(sectionDoc, section, personal.Has); err != nil {
					return Result{}, err
				}
				if strippedBlock, ok := sectionDoc.Block(section); ok {
					project.SetBlock(section, strings.TrimRight(strippedBlock, "\n"))
					anyImported = true
				} else {
					project.SetBlock(section, "")
				}
			}
			if anyImported || len(skipped) > 0 {
				result.ImportedFrom = nervConfigPath
			}
			result.SkippedPersonalKeys = skipped
		}
	}

	path := aiworkflow.ProjectConfigPath(deps.RepoRoot, workflowconfig.Tool)
	if err := os.MkdirAll(aiworkflow.ProjectDir(deps.RepoRoot), 0o755); err != nil {
		return Result{}, err
	}
	if err := writeDocument(path, project, deps.now()); err != nil {
		return Result{}, err
	}
	result.Path = path
	return result, nil
}

// loadOptionalDocument reads path as a *config.Document. A missing file
// is not an error: an empty document.
func loadOptionalDocument(path string) (*config.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config.Parse(""), nil
		}
		return nil, err
	}
	return config.Parse(string(data)), nil
}

// loadNervConfig reads nerv's own .ai-workflow/config.yaml. A missing
// file is not an error: found is false.
func loadNervConfig(path string) (*config.Document, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return config.Parse(string(data)), true, nil
}

// writeDocument writes doc to path, backing up any existing file first
// (a no-op, no write and no backup, when the rendered bytes already
// match).
func writeDocument(path string, doc *config.Document, now time.Time) error {
	newBytes := []byte(doc.String())
	previous, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return atomicfile.Write(path, newBytes, 0o644)
	}
	if string(previous) == string(newBytes) {
		return nil
	}
	_, err = atomicfile.Save(path, newBytes, atomicfile.Options{Now: now, BackupSuffix: "bak-init-"})
	return err
}
