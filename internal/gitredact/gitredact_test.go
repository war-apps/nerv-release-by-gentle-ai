package gitredact

import (
	"strings"
	"testing"
)

// A URL with userinfo (https://user:token@host/org/repo.git, common in
// CI) must never reach an error, a log line, a JSON report or a step's
// output with the token still in it. A bare ssh username
// (ssh://git@host, git's own login-name convention) is not a secret and
// is left unchanged; a password on any scheme, including ssh, is.
func TestURL_HidesUserinfo(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://x-access-token:ghs_abc123@github.com/acme/widgets.git", "https://***@github.com/acme/widgets.git"},
		{"https://bot:s3cr3t@github.com/acme/widgets.git", "https://***@github.com/acme/widgets.git"},
		// A bare ssh username carries no network credential.
		{"ssh://git@host:22/repo", "ssh://git@host:22/repo"},
		// ...but an ssh URL with a password part does.
		{"ssh://git:s3cr3t@host:22/repo", "ssh://***@host:22/repo"},
		// No userinfo: unchanged.
		{"https://github.com/acme/widgets.git", "https://github.com/acme/widgets.git"},
		{"", ""},
		// scp-like ssh syntax carries no network credential; left as it is.
		{"git@github.com:acme/widgets.git", "git@github.com:acme/widgets.git"},
		{"github.com:acme/widgets.git", "github.com:acme/widgets.git"},
		// A local path is not a URL at all.
		{"/srv/git/repo.git", "/srv/git/repo.git"},
		{`C:\git\repo.git`, `C:\git\repo.git`},
	}
	for _, tt := range tests {
		if got := URL(tt.in); got != tt.want {
			t.Errorf("URL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A scheme-bearing URL that net/url cannot parse (invalid
// percent-encoding, a bad port, ...) must still be redacted: URL fails
// closed through Stderr's text match instead of returning raw, and any
// credential it carries, unredacted just because url.Parse choked on it.
func TestURL_FailsClosedWhenUnparsable(t *testing.T) {
	in := "https://x-access-token:p%2wss@github.com/org/repo.git"
	got := URL(in)
	if strings.Contains(got, "p%2wss") || strings.Contains(got, "x-access-token") {
		t.Errorf("URL(%q) = %q, leaks the unparsable credential", in, got)
	}
}

// git itself echoes the remote URL it failed to reach in its stderr
// ("fatal: unable to access 'https://user:token@host/...'"); Stderr must
// mask it the same way URL does, wherever it appears in the text.
func TestStderr_MasksEmbeddedUserinfo(t *testing.T) {
	tests := []struct{ in, want string }{
		{
			"fatal: unable to access 'https://x-access-token:ghs_abc@github.com/a/b.git/': The requested URL returned error: 403",
			"fatal: unable to access 'https://***@github.com/a/b.git/': The requested URL returned error: 403",
		},
		{"fatal: repository 'https://github.com/a/b.git/' not found", "fatal: repository 'https://github.com/a/b.git/' not found"},
		{"", ""},
		{"fatal: could not read Username for 'git@github.com:a/b.git'", "fatal: could not read Username for 'git@github.com:a/b.git'"},
		// A bare ssh username echoed back in stderr is not a secret.
		{"fatal: could not read Username for 'ssh://git@host/a/b.git'", "fatal: could not read Username for 'ssh://git@host/a/b.git'"},
	}
	for _, tt := range tests {
		if got := Stderr(tt.in); got != tt.want {
			t.Errorf("Stderr(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A password containing '@' (https://user:p@ss@host/repo) must be masked
// in full: the userinfo runs up to the LAST '@' before the host, not the
// first one, or part of the password survives in free text.
func TestStderr_FullyMasksAPasswordContainingAt(t *testing.T) {
	in := "fatal: unable to access 'https://user:p@ss@github.com/org/repo.git/': error"
	got := Stderr(in)
	if strings.Contains(got, "p@ss") || strings.Contains(got, "ss@github") {
		t.Errorf("Stderr(%q) = %q, still leaks part of the password", in, got)
	}
	if want := "fatal: unable to access 'https://***@github.com/org/repo.git/': error"; got != want {
		t.Errorf("Stderr = %q, want %q", got, want)
	}
}
