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

// secretQueryRe matches a URL query parameter whose name says it holds a
// secret (token, access_token, private_token, password, secret, key,
// sig), introduced either by "?" (the URL's first parameter) or "&"
// (every later one). Matching at the very start of the string too lets
// maskQuery mask a bare query string (no leading "?"), the shape
// url.URL.RawQuery holds and the one URL rebuilds with when it hides a
// secret userinfo.
var secretQueryRe = regexp.MustCompile(`(?i)(^|[?&])([a-z0-9_.-]*(?:token|password|passwd|secret|key|sig)[a-z0-9_.-]*=)[^&\s'"#]+`)

// maskQuery masks every secret-looking query parameter value in s,
// wherever it appears (?access_token=x -> ?access_token=***).
func maskQuery(s string) string {
	return secretQueryRe.ReplaceAllString(s, "${1}${2}***")
}

// URL hides raw's userinfo when it may hold a secret (isSecretUserinfo)
// and masks every secret-looking query parameter value, the same way
// Stderr does: a scheme URL with credentials
// (https://user:token@host/org/repo.git) becomes
// https://***@host/org/repo.git. git's scp-like ssh syntax
// (git@host:org/repo, no scheme), a bare ssh username
// (ssh://git@host/...) and anything net/url cannot parse as a URL with a
// host and userinfo to hide have only their query masked. raw having a
// scheme but failing to parse (invalid percent-encoding, a bad port,
// ...) fails closed through Stderr's text match instead: it is never
// returned raw, credential included, just because net/url choked on it.
func URL(raw string) string {
	if raw == "" || !urlSchemeRe.MatchString(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Stderr(raw)
	}
	if u.Host == "" || u.User == nil || !isSecretUserinfo(u.Scheme, u.User.String()) {
		return maskQuery(raw)
	}
	// net/url decided raw is really a URL with a host and secret
	// userinfo to hide. The masked output is rebuilt from the parts
	// net/url already isolated (scheme, host, path, query, fragment),
	// never by re-running Stderr's free-text regex over raw: that regex
	// finds userinfo by scanning for "the last '@' before the first
	// '/'", a heuristic with nothing to bound it when raw has no path
	// and its query or fragment itself contains '@' (a mailto: value,
	// another embedded URL, ...) -- it would then wrongly swallow the
	// real host into the masked run. net/url's own fields never have
	// that ambiguity, so trust them instead of re-deriving the same
	// answer with a weaker tool.
	return maskQuery(rebuildWithMaskedUserinfo(u))
}

// rebuildWithMaskedUserinfo returns u with its userinfo replaced by ***:
// scheme + "://***@" + host + path, with its query kept (URL's caller
// masks it) and its fragment kept as it is, since neither is ever a
// credential.
func rebuildWithMaskedUserinfo(u *url.URL) string {
	out := u.Scheme + "://***@" + u.Host + u.EscapedPath()
	if u.ForceQuery || u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		out += "#" + u.EscapedFragment()
	}
	return out
}

// urlUserinfoRe matches a URL's scheme and userinfo embedded anywhere in
// free text ("scheme://userinfo@..."), such as a line of git's stderr.
// The userinfo group is greedy up to the LAST '@' before the first '/':
// a password may itself contain '@' (https://user:p@ss@host/...), and
// net/url resolves a multi-'@' userinfo the same way. The class excludes
// only '/': excluding whitespace or quotes too, as an earlier version of
// this regex did, let a userinfo containing either of those characters
// (a token with a quote in it, or a credential git echoes with a space
// inside it) through completely unmasked, because the whole match then
// failed to find any contiguous run ending right before the real '@'.
var urlUserinfoRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*)://([^/]*)@`)

// Stderr masks every secret-userinfo-bearing URL (isSecretUserinfo)
// embedded in s the same way URL does, wherever it appears, and masks
// every secret-looking query parameter value: git often echoes the
// remote URL, with its credentials, in the stderr of a failed clone,
// fetch or push.
func Stderr(s string) string {
	masked := urlUserinfoRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := urlUserinfoRe.FindStringSubmatch(m)
		scheme, userinfo := sub[1], sub[2]
		if !isSecretUserinfo(scheme, userinfo) {
			return m
		}
		return scheme + "://***@"
	})
	return maskQuery(masked)
}
