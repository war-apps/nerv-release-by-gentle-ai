package versionfile

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// yamlEntry is one open mapping key (or sequence item) of the indentation
// stack: the keys of the lines below it nest under it.
type yamlEntry struct {
	indent int
	key    string
	item   bool // a sequence item: keys under it have no path
}

// scanYAML walks the block mappings of a YAML document line by line, by
// indentation: plain, 'single' and "double" quoted scalars, comments and
// document markers. Keys inside sequences, block scalars (| and >) and
// flow collections are not reachable by a key path.
func scanYAML(b []byte) ([]value, error) {
	var (
		values []value
		stack  []yamlEntry
		// blockIndent is the indentation of the key that opened a block
		// scalar: lines indented deeper are its content.
		blockIndent = -1
	)
	for lineStart := 0; lineStart < len(b); {
		lineEnd := bytes.IndexByte(b[lineStart:], '\n')
		next := len(b)
		if lineEnd < 0 {
			lineEnd = len(b)
		} else {
			lineEnd += lineStart
			next = lineEnd + 1
		}
		if lineEnd > lineStart && b[lineEnd-1] == '\r' {
			lineEnd--
		}
		line := b[lineStart:lineEnd]
		pos := lineStart
		lineStart = next

		trimmed := bytes.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if len(bytes.TrimSpace(trimmed)) == 0 {
			continue
		}
		if blockIndent >= 0 {
			if indent > blockIndent {
				continue
			}
			blockIndent = -1
		}
		if trimmed[0] == '#' {
			continue
		}
		if bytes.HasPrefix(line, []byte("---")) || bytes.HasPrefix(line, []byte("...")) {
			stack = stack[:0]
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		if trimmed[0] == '-' && (len(trimmed) == 1 || trimmed[1] == ' ') {
			stack = append(stack, yamlEntry{indent: indent, item: true})
			continue
		}
		key, rest, ok := yamlKey(trimmed)
		if !ok {
			continue
		}
		reachable := true
		path := make([]string, 0, len(stack)+1)
		for _, e := range stack {
			if e.item {
				reachable = false
			}
			path = append(path, e.key)
		}
		path = append(path, key)
		stack = append(stack, yamlEntry{indent: indent, key: key})

		valStart := pos + indent + (len(trimmed) - len(rest))
		v := bytes.TrimLeft(rest, " \t")
		valStart += len(rest) - len(v)
		val := value{path: path}
		switch {
		case len(v) == 0 || v[0] == '#':
			val.notScalar = "a mapping, a sequence or empty"
		case v[0] == '|' || v[0] == '>':
			val.notScalar = "a block scalar"
			blockIndent = indent
		case v[0] == '[' || v[0] == '{':
			val.notScalar = "a flow collection"
		case v[0] == '"' || v[0] == '\'':
			end := yamlQuotedEnd(v)
			if end < 0 {
				return nil, fmt.Errorf("line at offset %d: unterminated quoted scalar", pos)
			}
			val.start, val.end = valStart+1, valStart+end-1
		default:
			end := len(v)
			if c := bytes.Index(v, []byte(" #")); c >= 0 {
				end = c
			}
			if c := bytes.Index(v, []byte("\t#")); c >= 0 && c < end {
				end = c
			}
			end = len(bytes.TrimRight(v[:end], " \t"))
			val.start, val.end = valStart, valStart+end
		}
		if reachable {
			values = append(values, val)
		}
	}
	return values, nil
}

// yamlKey splits "key: rest" (plain or quoted key) and reports whether the
// line is a mapping entry.
func yamlKey(line []byte) (key string, rest []byte, ok bool) {
	if line[0] == '"' || line[0] == '\'' {
		end := yamlQuotedEnd(line)
		if end < 0 || end >= len(line) || line[end] != ':' || (end+1 < len(line) && line[end+1] != ' ' && line[end+1] != '\t') {
			return "", nil, false
		}
		raw := string(line[:end])
		if line[0] == '"' {
			k, err := strconv.Unquote(raw)
			if err != nil {
				return "", nil, false
			}
			return k, line[end+1:], true
		}
		return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'"), line[end+1:], true
	}
	for i := 0; i < len(line); i++ {
		if line[i] == '#' && i > 0 && (line[i-1] == ' ' || line[i-1] == '\t') {
			return "", nil, false
		}
		if line[i] == ':' && (i+1 == len(line) || line[i+1] == ' ' || line[i+1] == '\t') {
			return strings.TrimRight(string(line[:i]), " \t"), line[i+1:], true
		}
	}
	return "", nil, false
}

// yamlQuotedEnd returns the offset just past the quoted scalar starting at
// s[0], or -1 when it is not closed on this line.
func yamlQuotedEnd(s []byte) int {
	q := s[0]
	for i := 1; i < len(s); i++ {
		switch {
		case q == '"' && s[i] == '\\':
			i++
		case q == '\'' && s[i] == '\'' && i+1 < len(s) && s[i+1] == '\'':
			i++
		case s[i] == q:
			return i + 1
		}
	}
	return -1
}
