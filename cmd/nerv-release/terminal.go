package main

import (
	"io"
	"os"

	"golang.org/x/term"
)

// stdinIsTerminal reports whether os.Stdin is attached to a real
// interactive terminal, via golang.org/x/term's real termios/console
// check. A std-library-only os.ModeCharDevice heuristic misreads a
// redirected stdin (such as `< /dev/null`) as a terminal on Windows Git
// Bash, which once made an interactive wizard run unattended against a
// developer machine in nerv; this stays on x/term instead.
//
// Wired into options.IsTerminal by defaultOptions; every other caller
// goes through that field, so tests inject a fixed outcome instead of
// depending on the real process's actual stdin.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// readAnswer reads one line from r a byte at a time, so nothing past the
// line is consumed, and reports whether it ended with a newline (false at
// EOF or on an error).
func readAnswer(r io.Reader) (string, bool) {
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n == 1 {
			if buf[0] == '\n' {
				return string(line), true
			}
			line = append(line, buf[0])
		}
		if err != nil {
			return string(line), false
		}
	}
}
