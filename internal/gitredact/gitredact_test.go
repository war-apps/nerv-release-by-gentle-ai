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

// A secret-looking query parameter (?access_token=..., ?token=...,
// ?password=...) must be masked the same way userinfo is: a CI remote or
// a git provider's API can carry its credential there instead of in
// userinfo, and nothing downstream should have to redact it again.
func TestURL_MasksSecretQueryParameters(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://github.com/acme/widgets.git?access_token=ghs_abc123", "https://github.com/acme/widgets.git?access_token=***"},
		{"https://github.com/acme/widgets.git?token=abc&ref=main", "https://github.com/acme/widgets.git?token=***&ref=main"},
		{"https://bot:s3cr3t@github.com/acme/widgets.git?password=hunter2", "https://***@github.com/acme/widgets.git?password=***"},
		// Not secret-looking: left alone.
		{"https://github.com/acme/widgets.git?ref=main", "https://github.com/acme/widgets.git?ref=main"},
	}
	for _, tt := range tests {
		if got := URL(tt.in); got != tt.want {
			t.Errorf("URL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Stderr must mask the same secret-looking query parameters as URL,
// wherever they appear in free text.
func TestStderr_MasksSecretQueryParameters(t *testing.T) {
	in := "fatal: unable to access 'https://github.com/a/b.git?access_token=ghs_abc&ref=main': 403"
	want := "fatal: unable to access 'https://github.com/a/b.git?access_token=***&ref=main': 403"
	if got := Stderr(in); got != want {
		t.Errorf("Stderr(%q) = %q, want %q", in, got, want)
	}
}

// When net/url finds secret userinfo, URL must rebuild the masked output
// from the parts it already parsed (scheme, host, path, query,
// fragment), never by re-running Stderr's free-text regex over raw. That
// regex finds userinfo by scanning for "the last '@' before the first
// '/'", a heuristic with nothing to bound it when the URL has no path
// and its query itself contains '@' (a mailto: value, another embedded
// URL, ...): it would then wrongly swallow the real host into the masked
// run instead of leaving it as it is.
func TestURL_RebuildsFromParsedPartsInsteadOfReapplyingTheRegex(t *testing.T) {
	in := "https://user:pass@host.example?redirect=mailto:a@b.com"
	want := "https://***@host.example?redirect=mailto:a@b.com"
	if got := URL(in); got != want {
		t.Errorf("URL(%q) = %q, want %q (a regex re-scan would wrongly swallow the host)", in, got, want)
	}
}

// Stderr's userinfo match must not exclude spaces or quotes from the
// class that finds "the last '@' before the first '/'": doing so lets a
// userinfo containing either of those characters (a token with a quote
// in it, or a credential git echoes with a space inside it) through
// completely unmasked, because the whole match then fails to find any
// contiguous run ending right before the real '@'.
func TestStderr_MasksUserinfoContainingSpaceOrQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{
			"fatal: unable to access 'https://us er:tok@host/a/b.git/': 403",
			"fatal: unable to access 'https://***@host/a/b.git/': 403",
		},
		{
			`fatal: unable to access 'https://user:to"ken@host/a/b.git/': 403`,
			"fatal: unable to access 'https://***@host/a/b.git/': 403",
		},
	}
	for _, tt := range tests {
		if got := Stderr(tt.in); got != tt.want {
			t.Errorf("Stderr(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
