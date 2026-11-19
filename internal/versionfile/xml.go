package versionfile

import (
	"bytes"
	"fmt"
)

// xmlElement is one open element: its path from the root and where its
// content starts.
type xmlElement struct {
	path         []string
	contentStart int
	hasChildren  bool
}

// scanXML records every element's text content by its slash path from the
// root element, skipping comments, CDATA sections, processing
// instructions and declarations. An element with child elements is not a
// scalar.
func scanXML(b []byte) ([]value, error) {
	var (
		values []value
		stack  []*xmlElement
	)
	for i := 0; i < len(b); {
		lt := bytes.IndexByte(b[i:], '<')
		if lt < 0 {
			break
		}
		i += lt
		switch {
		case bytes.HasPrefix(b[i:], []byte("<!--")):
			end := bytes.Index(b[i+4:], []byte("-->"))
			if end < 0 {
				return nil, fmt.Errorf("offset %d: unterminated comment", i)
			}
			i += 4 + end + 3
		case bytes.HasPrefix(b[i:], []byte("<![CDATA[")):
			end := bytes.Index(b[i:], []byte("]]>"))
			if end < 0 {
				return nil, fmt.Errorf("offset %d: unterminated CDATA section", i)
			}
			i += end + 3
		case bytes.HasPrefix(b[i:], []byte("<?")):
			end := bytes.Index(b[i:], []byte("?>"))
			if end < 0 {
				return nil, fmt.Errorf("offset %d: unterminated processing instruction", i)
			}
			i += end + 2
		case bytes.HasPrefix(b[i:], []byte("<!")):
			end := xmlTagEnd(b, i)
			if end < 0 {
				return nil, fmt.Errorf("offset %d: unterminated declaration", i)
			}
			i = end
		case bytes.HasPrefix(b[i:], []byte("</")):
			end := xmlTagEnd(b, i)
			if end < 0 {
				return nil, fmt.Errorf("offset %d: unterminated end tag", i)
			}
			name := string(bytes.TrimSpace(b[i+2 : end-1]))
			if len(stack) == 0 || stack[len(stack)-1].path[len(stack[len(stack)-1].path)-1] != name {
				return nil, fmt.Errorf("offset %d: unexpected </%s>", i, name)
			}
			e := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			v := value{path: e.path}
			if e.hasChildren {
				v.notScalar = "an element with child elements"
			} else {
				content := b[e.contentStart:i]
				lead := len(content) - len(bytes.TrimLeft(content, " \t\r\n"))
				v.start = e.contentStart + lead
				v.end = e.contentStart + len(bytes.TrimRight(content, " \t\r\n"))
				if v.end < v.start {
					v.end = v.start
				}
			}
			values = append(values, v)
			i = end
		default:
			end := xmlTagEnd(b, i)
			if end < 0 {
				return nil, fmt.Errorf("offset %d: unterminated start tag", i)
			}
			nameEnd := i + 1
			for nameEnd < end && !bytes.ContainsAny(b[nameEnd:nameEnd+1], " \t\r\n/>") {
				nameEnd++
			}
			path := []string{string(b[i+1 : nameEnd])}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.hasChildren = true
				path = append(append([]string{}, parent.path...), path...)
			}
			if b[end-2] == '/' {
				values = append(values, value{path: path, notScalar: "an empty element"})
			} else {
				stack = append(stack, &xmlElement{path: path, contentStart: end})
			}
			i = end
		}
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("element <%s> is not closed", stack[len(stack)-1].path[len(stack[len(stack)-1].path)-1])
	}
	return values, nil
}

// xmlTagEnd returns the offset just past the '>' closing the tag at i,
// skipping quoted attribute values, or -1.
func xmlTagEnd(b []byte, i int) int {
	var quote byte
	for j := i + 1; j < len(b); j++ {
		switch c := b[j]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return j + 1
		}
	}
	return -1
}
