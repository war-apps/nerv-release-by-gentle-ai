// Package gitredact hides the credentials a git remote URL carries
// before it reaches an error, a log line, a JSON report or a step's
// output. A URL with userinfo (https://user:token@host/org/repo.git, the
// shape CI commonly configures origin with) otherwise leaks the token
// verbatim wherever nerv-release formats that URL, or formats git's own
// stderr (which often echoes the URL it failed to reach).
package gitredact

import (
	"net/url"
	"regexp"
	"strings"
)

// urlSchemeRe matches a URL scheme (scheme://...). git's scp-like ssh
// syntax ([user@]host:path) and a local path have no "://" and never
// match: neither carries a network credential, so URL leaves them alone.
var urlSchemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

// isSecretUserinfo reports whether a URL's userinfo may hold a secret:
// any userinfo on a scheme other than ssh (https://<token>@host is a
// common CI form with no password part at all), or a password part on
// any scheme. A bare ssh username (ssh://git@host, git@host:org/repo) is
// git's own convention for the login name, not a secret.
func isSecretUserinfo(scheme, userinfo string) bool {
	switch strings.ToLower(scheme) {
	case "ssh", "git+ssh", "ssh+git", "git":
		return strings.Contains(userinfo, ":")
	default:
		return true
	}
}

// URL hides raw's userinfo when it may hold a secret (isSecretUserinfo):
// a scheme URL with credentials (https://user:token@host/org/repo.git)
// becomes https://***@host/org/repo.git. git's scp-like ssh syntax
// (git@host:org/repo, no scheme), a bare ssh username
// (ssh://git@host/...) and anything net/url cannot parse as a URL with a
// host and userinfo to hide are returned unchanged. raw having a scheme
// but failing to parse (invalid percent-encoding, a bad port, ...) fails
// closed through Stderr's text match instead: it is never returned raw,
// credential included, just because net/url choked on it.
func URL(raw string) string {
	if raw == "" || !urlSchemeRe.MatchString(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Stderr(raw)
	}
	if u.Host == "" || u.User == nil || !isSecretUserinfo(u.Scheme, u.User.String()) {
		return raw
	}
	// net/url decided raw is really a URL with a host and secret
	// userinfo to hide; the substitution itself reuses Stderr's regex
	// rather than url.URL.String(), which would percent-encode "***" and
	// so change bytes Stderr (and the rest of raw) never touches.
	return Stderr(raw)
}

// urlUserinfoRe matches a URL's scheme and userinfo embedded anywhere in
// free text ("scheme://userinfo@..."), such as a line of git's stderr.
// The userinfo group is greedy up to the LAST '@' before the host: a
// password may itself contain '@' (https://user:p@ss@host/...), and
// net/url resolves a multi-'@' userinfo the same way.
var urlUserinfoRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*)://([^/\s'"]*)@`)

// Stderr masks every secret-userinfo-bearing URL (isSecretUserinfo)
// embedded in s the same way URL does, wherever it appears: git often
// echoes the remote URL, with its credentials, in the stderr of a failed
// clone, fetch or push.
func Stderr(s string) string {
	return urlUserinfoRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := urlUserinfoRe.FindStringSubmatch(m)
		scheme, userinfo := sub[1], sub[2]
		if !isSecretUserinfo(scheme, userinfo) {
			return m
		}
		return scheme + "://***@"
	})
}
