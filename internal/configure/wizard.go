package configure

import (
	"fmt"
	"io"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/config"
	"github.com/war-apps/nerv-release-by-gentle-ai/internal/provider"
)

// WizardResult reports what RunWizard did.
type WizardResult struct {
	Changed   bool
	Backup    string
	ChangeLog []string
}

// RunWizard drives nerv-release configure's interactive wizard: for
// every known provider that owns at least one scope: user key, asks
// whether to configure it now, and for each of its scope: user keys,
// prompts with the manifest's prompt and current value or default
// (Enter keeps it). A provider with no scope: user key at all (most of
// the 6) is never even asked about. It then shows a diff of every
// changed key and writes the global release.yaml only on confirmation.
//
// in is read one line at a time: a real terminal, or an --answers file
// (one answer per line). A genuinely exhausted reader before the wizard
// finishes returns ErrInputClosed and writes nothing -- a blank line is
// always "keep current", never treated the same as EOF.
func RunWizard(deps Deps, in io.Reader, out io.Writer) (WizardResult, error) {
	s := newSession(in, out)

	providers, err := Providers(deps.ExtensionsFS)
	if err != nil {
		return WizardResult{}, err
	}
	doc, previous, err := loadDocument(deps.GlobalPath)
	if err != nil {
		return WizardResult{}, err
	}

	fmt.Fprintln(out, "--- nerv-release configuration ---")
	fmt.Fprintf(out, "File: %s\n\n", deps.GlobalPath)

	var changeLog []string
	record := func(label, oldValue, newValue string) {
		if newValue != oldValue {
			changeLog = append(changeLog, fmt.Sprintf("%s: %s -> %s", label, dashIfEmpty(oldValue), dashIfEmpty(newValue)))
		}
	}

	fmt.Fprintln(out, "--- Providers ---")
	anyConfigurable := false
	for _, p := range providers {
		keys := userScopeKeys(p)
		if len(keys) == 0 {
			continue
		}
		anyConfigurable = true
		defaultConfigure := anyAlreadySet(doc, p, keys)
		configureIt, err := s.yesNo(fmt.Sprintf("Configure %s?", p.Name), defaultConfigure)
		if err != nil {
			return WizardResult{}, err
		}
		if !configureIt {
			continue
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "-- %s --\n", p.Name)
		for _, c := range keys {
			if !c.Scalar() {
				continue
			}
			path := userKeyPath(p, c.Key)
			old, found := doc.Scalar(path)
			if !found {
				old = ""
			}
			def := old
			if def == "" {
				def = c.Default
			}
			answer, err := askTyped(s, c.Prompt, c.Type, def)
			if err != nil {
				return WizardResult{}, err
			}
			if answer == def {
				continue
			}
			doc.SetScalar(path, answer)
			record(p.Name+"."+c.Key, old, answer)
		}
	}
	if !anyConfigurable {
		fmt.Fprintln(out, "No provider has a user-scope setting; nothing to configure.")
	}

	fmt.Fprintln(out)
	if len(changeLog) == 0 {
		fmt.Fprintf(out, "No changes; %s left untouched.\n", deps.GlobalPath)
		return WizardResult{}, nil
	}
	fmt.Fprintln(out, "--- Summary ---")
	for _, c := range changeLog {
		fmt.Fprintf(out, "  %s\n", c)
	}
	write, err := s.yesNo(fmt.Sprintf("Write to %s?", deps.GlobalPath), true)
	if err != nil {
		return WizardResult{}, err
	}
	if !write {
		fmt.Fprintln(out, "Aborted; nothing written.")
		return WizardResult{ChangeLog: changeLog}, nil
	}

	if err := saveDocument(deps.GlobalPath, doc, previous, deps.now()); err != nil {
		return WizardResult{}, err
	}
	fmt.Fprintf(out, "Written: %s\n", deps.GlobalPath)
	return WizardResult{Changed: true, ChangeLog: changeLog}, nil
}

// anyAlreadySet reports whether any of provider p's keys already holds
// a value in doc, so the wizard's "Configure <name>?" defaults to yes
// once the user has configured it before.
func anyAlreadySet(doc *config.Document, p *provider.Provider, keys []provider.ConfigKey) bool {
	for _, c := range keys {
		if _, found := doc.Scalar(userKeyPath(p, c.Key)); found {
			return true
		}
	}
	return false
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
