package versionfile

import (
	"fmt"
	"strconv"
	"strings"
)

// tomlScanner walks a TOML document: [table] headers, dotted keys, basic
// and literal strings (single and multi-line), bare values, and inline
// tables and arrays (which it skips). Keys under an [[array table]] have no
// path.
type tomlScanner struct {
	b      []byte
	i      int
	values []value
}

func scanTOML(b []byte) ([]value, error) {
	s := &tomlScanner{b: b}
	table := []string{}
	for {
		s.skipBlank(true)
		if s.i >= len(s.b) {
			return s.values, nil
		}
		if s.b[s.i] == '[' {
			array := s.i+1 < len(s.b) && s.b[s.i+1] == '['
			if array {
				s.i += 2
			} else {
				s.i++
			}
			key, err := s.key()
			if err != nil {
				return nil, err
			}
			closing := "]"
			if array {
				closing = "]]"
			}
			s.spaces()
			if !strings.HasPrefix(string(s.b[s.i:]), closing) {
				return nil, s.errorf("expected %q", closing)
			}
			s.i += len(closing)
			table = key
			if array {
				table = nil
			}
		} else {
			key, err := s.key()
			if err != nil {
				return nil, err
			}
			s.spaces()
			if s.i >= len(s.b) || s.b[s.i] != '=' {
				return nil, s.errorf("expected '='")
			}
			s.i++
			s.spaces()
			var path []string
			if table != nil {
				path = append(append([]string{}, table...), key...)
			}
			if err := s.value(path); err != nil {
				return nil, err
			}
		}
		s.spaces()
		s.comment()
		if s.i < len(s.b) && s.b[s.i] != '\n' && s.b[s.i] != '\r' {
			return nil, s.errorf("unexpected %q at the end of a line", s.b[s.i])
		}
	}
}

func (s *tomlScanner) errorf(format string, a ...any) error {
	return fmt.Errorf("offset %d: %s", s.i, fmt.Sprintf(format, a...))
}

func (s *tomlScanner) spaces() {
	for s.i < len(s.b) && (s.b[s.i] == ' ' || s.b[s.i] == '\t') {
		s.i++
	}
}

func (s *tomlScanner) comment() {
	if s.i < len(s.b) && s.b[s.i] == '#' {
		for s.i < len(s.b) && s.b[s.i] != '\n' {
			s.i++
		}
	}
}

// skipBlank skips spaces and comments, and line breaks when newlines.
func (s *tomlScanner) skipBlank(newlines bool) {
	for s.i < len(s.b) {
		s.spaces()
		s.comment()
		if newlines && s.i < len(s.b) && (s.b[s.i] == '\n' || s.b[s.i] == '\r') {
			s.i++
			continue
		}
		return
	}
}

// key scans a dotted key: bare, "basic" or 'literal' segments.
func (s *tomlScanner) key() ([]string, error) {
	var segs []string
	for {
		s.spaces()
		if s.i >= len(s.b) {
			return nil, s.errorf("expected a key")
		}
		switch c := s.b[s.i]; {
		case c == '"' || c == '\'':
			start := s.i
			end, err := s.quoted(string(c))
			if err != nil {
				return nil, err
			}
			seg := string(s.b[start+1 : end-1])
			if c == '"' {
				if seg, err = strconv.Unquote(string(s.b[start:end])); err != nil {
					return nil, s.errorf("invalid key %s", s.b[start:end])
				}
			}
			segs = append(segs, seg)
		default:
			start := s.i
			for s.i < len(s.b) && isBareKey(s.b[s.i]) {
				s.i++
			}
			if s.i == start {
				return nil, s.errorf("expected a key, found %q", c)
			}
			segs = append(segs, string(s.b[start:s.i]))
		}
		s.spaces()
		if s.i >= len(s.b) || s.b[s.i] != '.' {
			return segs, nil
		}
		s.i++
	}
}

func isBareKey(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// quoted scans a string whose opening delimiter q starts at s.i (a basic
// or literal quote, single or tripled) and returns the offset just past its
// closing delimiter.
func (s *tomlScanner) quoted(q string) (int, error) {
	s.i += len(q)
	for s.i < len(s.b) {
		if q[0] == '"' && s.b[s.i] == '\\' {
			s.i += 2
			continue
		}
		if len(q) == 1 && s.b[s.i] == '\n' {
			break
		}
		if strings.HasPrefix(string(s.b[s.i:min(s.i+len(q), len(s.b))]), q) {
			s.i += len(q)
			return s.i, nil
		}
		s.i++
	}
	return 0, s.errorf("unterminated string")
}

// value scans the value of a key at s.i; path is nil when unreachable.
func (s *tomlScanner) value(path []string) error {
	if s.i >= len(s.b) {
		return s.errorf("expected a value")
	}
	start := s.i
	switch c := s.b[s.i]; c {
	case '"', '\'':
		q := string(c)
		if strings.HasPrefix(string(s.b[s.i:min(s.i+3, len(s.b))]), strings.Repeat(q, 3)) {
			q = strings.Repeat(q, 3)
		}
		end, err := s.quoted(q)
		if err != nil {
			return err
		}
		s.record(path, start+len(q), end-len(q), "", "")
	case '[', '{':
		if err := s.skipNested(); err != nil {
			return err
		}
		kind := "an array"
		if c == '{' {
			kind = "an inline table"
		}
		s.record(path, start, start, "", kind)
	default:
		for s.i < len(s.b) && !strings.ContainsRune(" \t\r\n#,]}", rune(s.b[s.i])) {
			s.i++
		}
		if s.i == start {
			return s.errorf("expected a value")
		}
		s.record(path, start, s.i, `"`, "")
	}
	return nil
}

func (s *tomlScanner) record(path []string, start, end int, wrap, notScalar string) {
	if path != nil {
		s.values = append(s.values, value{path: path, start: start, end: end, wrap: wrap, notScalar: notScalar})
	}
}

// skipNested skips an array or inline table, with the strings and
// comments inside it.
func (s *tomlScanner) skipNested() error {
	depth := 0
	for s.i < len(s.b) {
		switch c := s.b[s.i]; c {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				s.i++
				return nil
			}
		case '#':
			s.comment()
			continue
		case '"', '\'':
			q := string(c)
			if strings.HasPrefix(string(s.b[s.i:min(s.i+3, len(s.b))]), strings.Repeat(q, 3)) {
				q = strings.Repeat(q, 3)
			}
			if _, err := s.quoted(q); err != nil {
				return err
			}
			continue
		}
		s.i++
	}
	return s.errorf("unterminated array or inline table")
}
