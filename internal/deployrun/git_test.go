package deployrun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/war-apps/nerv-release-by-gentle-ai/internal/proc"
)

// fakeRunner answers every Run call with result and err, regardless of
// the command: enough to exercise git()'s and clone()'s error formatting
// without a real git process or network access.
type fakeRunner struct {
	result proc.Result
	err    error
}

func (f fakeRunner) Run(context.Context, proc.Command) (proc.Result, error) {
	return f.result, f.err
}

const credentialSecret = "ghs_super_secret_token"

// A CI remote commonly configures origin as
// https://x-access-token:<token>@host/org/repo.git; git itself echoes
// that URL, token included, in the stderr of a failed command (a push
// rejected, a fetch that cannot authenticate, ...). git() must never let
// that token reach the error it returns.
func TestWorkspaceGit_RedactsCredentialsFromStderr(t *testing.T) {
	stderr := "fatal: unable to access 'https://x-access-token:" + credentialSecret +
		"@github.com/acme/widgets.git/': The requested URL returned error: 403"
	w := workspace{runner: fakeRunner{result: proc.Result{Stderr: []byte(stderr)}, err: errors.New("exit status 128")}}

	_, err := w.git(context.Background(), "fetch", "--quiet", "--tags", "origin")
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), credentialSecret) {
		t.Errorf("git() leaks the credential: %v", err)
	}
	if !strings.Contains(err.Error(), "***") {
		t.Errorf("git() does not redact the credentialed URL: %v", err)
	}
}

// clone formats the URL it was given directly into its own error (never
// through git()'s stderr-only redaction), so it needs the same
// protection for the URL argument itself, on top of git's stderr, which
// usually echoes that same URL back.
func TestWorkspaceClone_RedactsCredentialsFromURLAndStderr(t *testing.T) {
	credentialed := "https://x-access-token:" + credentialSecret + "@github.com/acme/widgets.git"
	stderr := "fatal: unable to access '" + credentialed + "/': Failed to connect to github.com port 443"
	w := workspace{runner: fakeRunner{result: proc.Result{Stderr: []byte(stderr)}, err: errors.New("exit status 128")}, dir: t.TempDir()}

	err := w.clone(context.Background(), credentialed, "main")
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), credentialSecret) {
		t.Errorf("clone() leaks the credential: %v", err)
	}
	if !strings.Contains(err.Error(), "***") {
		t.Errorf("clone() does not redact the credentialed URL: %v", err)
	}
}

// git's scp-like ssh syntax (git@host:org/repo) carries no network
// credential (at most a username): git() and clone() must leave it, and
// any stderr naming it, exactly as they are.
func TestWorkspaceGitAndClone_LeaveScpLikeAddressesUnchanged(t *testing.T) {
	const scpLike = "git@github.com:acme/widgets.git"
	stderr := "fatal: could not read Username for '" + scpLike + "': terminal prompts disabled"
	w := workspace{runner: fakeRunner{result: proc.Result{Stderr: []byte(stderr)}, err: errors.New("exit status 128")}, dir: t.TempDir()}

	if _, err := w.git(context.Background(), "fetch", "--tags", "origin"); err == nil || !strings.Contains(err.Error(), scpLike) {
		t.Errorf("git() = %v, want the scp-like address %q unchanged", err, scpLike)
	}
	if err := w.clone(context.Background(), scpLike, "main"); err == nil || !strings.Contains(err.Error(), scpLike) {
		t.Errorf("clone() = %v, want the scp-like address %q unchanged", err, scpLike)
	}
}

// The note is JSON.stringify({channels: [channel]}): null for the default
// channel, no HTML escaping.
func TestChannelNote_IsSemanticReleases(t *testing.T) {
	for channel, want := range map[string]string{"": `{"channels":[null]}`, "beta": `{"channels":["beta"]}`, "a<b": `{"channels":["a<b"]}`} {
		if got := channelNote(channel); got != want {
			t.Errorf("channelNote(%q) = %s, want %s", channel, got, want)
		}
	}
}

// The message is compared as git commit -m stores it.
func TestCleanupWhitespace_IsGitsWhitespaceMode(t *testing.T) {
	in := "\n\nchore(release): 1.0.0 [skip ci]  \n\n\n## 1.0.0\t\n\n* a\n\n"
	if got, want := cleanupWhitespace(in), "chore(release): 1.0.0 [skip ci]\n\n## 1.0.0\n\n* a"; got != want {
		t.Errorf("cleanup %q, want %q", got, want)
	}
}
