// Package commands contains all CLI subcommand implementations.
package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// promptLine reads a single line of text from r after printing prompt to w.
func promptLine(w io.Writer, r io.Reader, prompt string) (string, error) {
	fmt.Fprint(w, prompt)
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", io.EOF
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
