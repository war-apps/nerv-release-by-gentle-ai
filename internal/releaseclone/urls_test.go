package releaseclone

import (
	"strings"
	"testing"
)

// redact itself moved into gitredact (its URL and Stderr are now the
// package's only redaction); stderrTail, a call site of Stderr, must
// still mask a secret-looking query parameter the same way userinfo is
// masked, with nothing left redacting it a second time.
func TestStderrTail_MasksSecretQueryParameters(t *testing.T) {
	in := []byte("fatal: unable to access 'https://h/r.git?access_token=abc123&ref=main': 403")
	got := stderrTail(in)
	if strings.Contains(got, "abc123") {
		t.Errorf("stderrTail(%q) leaks the query token: %q", in, got)
	}
	if !strings.Contains(got, "access_token=***") {
		t.Errorf("stderrTail(%q) = %q, want the query token masked", in, got)
	}
}

// pushAddress's refusal for an unsupported scheme names raw itself
// (gitredact.URL, another call site of the package's only redaction);
// it must mask a secret-looking query parameter the same way.
func TestPushAddress_MasksSecretQueryParametersInTheRefusal(t *testing.T) {
	raw := "ftp://example.com/w.git?token=abc123"
	_, err := pushAddress(raw, raw)
	if err == nil {
		t.Fatal("want an error for an unsupported scheme")
	}
	if strings.Contains(err.Error(), "abc123") {
		t.Errorf("pushAddress error leaks the query token: %v", err)
	}
	if !strings.Contains(err.Error(), "token=***") {
		t.Errorf("pushAddress error = %v, want the query token masked", err)
	}
}

func TestPublicURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://github.com/example/repo.git", "https://github.com/example/repo.git"},
		{"https://nerv:s3cr3t@github.com/example/repo.git?access_token=tok#frag", "https://github.com/example/repo.git"},
		{"http://git.example.com:8080/team/repo", "http://git.example.com:8080/team/repo"},
		{"ssh://git@github.com/example/repo.git", "ssh://github.com/example/repo.git"},
		{"git+ssh://git@github.com/example/repo.git", "git+ssh://github.com/example/repo.git"},
		{"git://github.com/example/repo.git", "git://github.com/example/repo.git"},
		{"git@github.com:example/repo.git", "git@github.com:example/repo.git"},
		{"github.com:example/repo.git", "github.com:example/repo.git"},
		// Local repositories have no public address for release-notes links.
		{"", ""},
		{"../origin.git", ""},
		{"/srv/git/repo.git", ""},
		{`C:\git\repo.git`, ""},
		{"file:///srv/git/repo.git", ""},
		{"https://", ""},
	}
	for _, tt := range tests {
		if got := publicURL(tt.in); got != tt.want {
			t.Errorf("publicURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestReleaseURL(t *testing.T) {
	tests := []struct {
		name, public, tag, want string
	}{
		{"https with .git", "https://github.com/acme/widgets.git", "v1.2.0", "https://github.com/acme/widgets/releases/tag/v1.2.0"},
		{"https without .git", "https://github.com/acme/widgets", "api-v1.0.0", "https://github.com/acme/widgets/releases/tag/api-v1.0.0"},
		{"scp-like ssh", "git@github.com:acme/widgets.git", "v2.0.0-alpha.1", "https://github.com/acme/widgets/releases/tag/v2.0.0-alpha.1"},
		{"ssh URL with port", "ssh://github.com:22/acme/widgets.git", "v1.0.0", "https://github.com/acme/widgets/releases/tag/v1.0.0"},
		{"http keeps its scheme and port", "http://git.local:3000/acme/widgets", "v1.0.0", "http://git.local:3000/acme/widgets/releases/tag/v1.0.0"},
		{"tag with a slash", "https://github.com/acme/widgets", "web/v1.0.0", "https://github.com/acme/widgets/releases/tag/web/v1.0.0"},
		{"tag escaped", "https://github.com/acme/widgets", "v1.0.0+build 1", "https://github.com/acme/widgets/releases/tag/v1.0.0+build%201"},
		{"no origin", "", "v1.0.0", ""},
		{"no tag", "https://github.com/acme/widgets", "", ""},
		{"no repository path", "https://github.com/", "v1.0.0", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReleaseURL(tt.public, tt.tag); got != tt.want {
				t.Errorf("ReleaseURL(%q, %q) = %q, want %q", tt.public, tt.tag, got, tt.want)
			}
		})
	}
}

func TestPushAddress(t *testing.T) {
	tests := []struct {
		name, raw, resolved, want string
		wantErr                   bool
	}{
		{"public https", "https://github.com/acme/w.git", "https://github.com/acme/w.git", "https://github.com/acme/w.git", false},
		{"credentials dropped", "https://bot:s3cr3t@github.com/acme/w.git", "https://bot:s3cr3t@github.com/acme/w.git", "https://github.com/acme/w.git", false},
		{"scp-like", "git@github.com:acme/w.git", "git@github.com:acme/w.git", "git@github.com:acme/w.git", false},
		{"local path", "../origin.git", "/repos/origin.git", "/repos/origin.git", false},
		{"file URL", "file:///repos/origin.git", "file:///repos/origin.git", "file:///repos/origin.git", false},
		{"unsupported scheme", "ftp://example.com/w.git", "ftp://example.com/w.git", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pushAddress(tt.raw, tt.resolved)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("pushAddress(%q) = %q, %v; want %q (error %v)", tt.raw, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

// workspaceRunner fakes a repository whose origin is originURL: the clone
// creates its directory (so a release: section can be written into it), and
