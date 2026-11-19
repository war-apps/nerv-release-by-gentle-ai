package versionfile

import "bytes"

// scanProperties records every `key=value`, `key: value` and `key value`
// line of a Java properties file by its key. Comment lines (# or !) and
// the continuation lines of a value ending in a backslash are skipped; a
// continued value is not a scalar this package can rewrite.
func scanProperties(b []byte) ([]value, error) {
	var values []value
	continued := false
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

		wasContinued := continued
		continued = endsInBackslash(line)
		if wasContinued {
			continue
		}
		i := 0
		for i < len(line) && isPropSpace(line[i]) {
			i++
		}
		if i == len(line) || line[i] == '#' || line[i] == '!' {
			continued = false
			continue
		}
		keyStart := i
		for i < len(line) && line[i] != '=' && line[i] != ':' && !isPropSpace(line[i]) {
			if line[i] == '\\' {
				i++
			}
			i++
		}
		key := string(line[keyStart:min(i, len(line))])
		for i < len(line) && isPropSpace(line[i]) {
			i++
		}
		if i < len(line) && (line[i] == '=' || line[i] == ':') {
			i++
			for i < len(line) && isPropSpace(line[i]) {
				i++
			}
		}
		v := value{path: []string{key}, start: pos + i, end: pos + len(bytes.TrimRight(line, " \t\f"))}
		if continued {
			v.notScalar = "a value continued on the next line"
		}
		if v.end < v.start {
			v.end = v.start
		}
		values = append(values, v)
	}
	return values, nil
}

func isPropSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\f' }

// endsInBackslash reports whether line ends in an odd number of
// backslashes, which continues it on the next line.
func endsInBackslash(line []byte) bool {
	n := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}
