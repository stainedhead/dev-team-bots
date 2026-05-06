// Package commands contains all CLI subcommand implementations.
package commands

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// promptLine reads a single line of text from r after printing prompt to w.
// Reads byte-by-byte to avoid consuming lookahead from a shared reader.
func promptLine(w io.Writer, r io.Reader, prompt string) (string, error) {
	fmt.Fprint(w, prompt)
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			sb.WriteByte(buf[0])
		}
		if err == io.EOF {
			if sb.Len() > 0 {
				break
			}
			return "", io.EOF
		}
		if err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

// promptPassword reads a password from r (without echo in a real terminal).
// In tests r is a plain strings.Reader so we just read a line.
func promptPassword(w io.Writer, r io.Reader, prompt string) (string, error) {
	// Try to use term.ReadPassword when r is an actual tty (os.Stdin).
	// In tests, r is a non-tty reader, so we fall back to plain line reading.
	if f, ok := r.(*os.File); ok && isTerminal(f) {
		return readTerminalPassword(w, f, prompt)
	}
	return promptLine(w, r, prompt)
}

// confirm prints a [y/N] prompt and returns true if the user typed "y" or "Y".
func confirm(w io.Writer, r io.Reader, prompt string) (bool, error) {
	answer, err := promptLine(w, r, prompt)
	if err != nil {
		return false, err
	}
	return strings.ToLower(strings.TrimSpace(answer)) == "y", nil
}
