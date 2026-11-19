package versionfile

import (
	"encoding/json"
	"fmt"
)

// jsonScanner walks a JSON document and records every value reachable
// through object keys (values inside arrays have no key path).
type jsonScanner struct {
	b      []byte
	i      int
	values []value
}

func scanJSON(b []byte) ([]value, error) {
	s := &jsonScanner{b: b}
	s.ws()
	if err := s.value([]string{}); err != nil {
		return nil, err
	}
	s.ws()
	if s.i != len(s.b) {
		return nil, s.errorf("unexpected data after the document")
	}
	return s.values, nil
}

func (s *jsonScanner) errorf(format string, a ...any) error {
	return fmt.Errorf("offset %d: %s", s.i, fmt.Sprintf(format, a...))
}

func (s *jsonScanner) ws() {
	for s.i < len(s.b) && (s.b[s.i] == ' ' || s.b[s.i] == '\t' || s.b[s.i] == '\r' || s.b[s.i] == '\n') {
		s.i++
	}
}

// value scans one value at s.i; path is nil inside arrays.
func (s *jsonScanner) value(path []string) error {
	if s.i >= len(s.b) {
		return s.errorf("unexpected end")
	}
	start := s.i
	switch c := s.b[s.i]; {
	case c == '{':
		s.record(path, start, start, "", "an object")
		return s.object(path)
	case c == '[':
		s.record(path, start, start, "", "an array")
		return s.array()
	case c == '"':
		if _, err := s.str(); err != nil {
			return err
		}
		s.record(path, start+1, s.i-1, "", "")
	default:
		for s.i < len(s.b) && !isJSONDelim(s.b[s.i]) {
			s.i++
		}
		if s.i == start {
			return s.errorf("unexpected %q", c)
		}
		s.record(path, start, s.i, `"`, "")
	}
	return nil
}

func isJSONDelim(c byte) bool {
	return c == ',' || c == '}' || c == ']' || c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func (s *jsonScanner) record(path []string, start, end int, wrap, notScalar string) {
	if path != nil {
		s.values = append(s.values, value{path: path, start: start, end: end, wrap: wrap, notScalar: notScalar})
	}
}

// str scans a string at s.i and returns its decoded text.
func (s *jsonScanner) str() (string, error) {
	start := s.i
	s.i++
	for s.i < len(s.b) && s.b[s.i] != '"' {
		if s.b[s.i] == '\\' {
			s.i++
		}
		s.i++
	}
	if s.i >= len(s.b) {
		return "", s.errorf("unterminated string")
	}
	s.i++
	var text string
	if err := json.Unmarshal(s.b[start:s.i], &text); err != nil {
		return "", s.errorf("invalid string: %v", err)
	}
	return text, nil
}

func (s *jsonScanner) object(path []string) error {
	s.i++
	s.ws()
	if s.i < len(s.b) && s.b[s.i] == '}' {
		s.i++
		return nil
	}
	for {
		s.ws()
		if s.i >= len(s.b) || s.b[s.i] != '"' {
			return s.errorf("expected a key")
		}
		key, err := s.str()
		if err != nil {
			return err
		}
		s.ws()
		if s.i >= len(s.b) || s.b[s.i] != ':' {
			return s.errorf("expected ':'")
		}
		s.i++
		s.ws()
		var child []string
		if path != nil {
			child = append(append([]string{}, path...), key)
		}
		if err := s.value(child); err != nil {
			return err
		}
		if done, err := s.next('}'); done || err != nil {
			return err
		}
	}
}

func (s *jsonScanner) array() error {
	s.i++
	s.ws()
	if s.i < len(s.b) && s.b[s.i] == ']' {
		s.i++
		return nil
	}
	for {
		s.ws()
		if err := s.value(nil); err != nil {
			return err
		}
		if done, err := s.next(']'); done || err != nil {
			return err
		}
	}
}

// next consumes a ',' (more members follow) or the closing byte.
func (s *jsonScanner) next(closing byte) (bool, error) {
	s.ws()
	if s.i >= len(s.b) {
		return false, s.errorf("unexpected end")
	}
	switch s.b[s.i] {
	case ',':
		s.i++
		return false, nil
	case closing:
		s.i++
		return true, nil
	}
	return false, s.errorf("expected ',' or %q", closing)
}
