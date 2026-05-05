package commands

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// isTerminal reports whether f is a terminal.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// readTerminalPassword reads a password from a real terminal with echo suppressed.
func readTerminalPassword(w io.Writer, f *os.File, prompt string) (string, error) {
	fmt.Fprint(w, prompt)
	pw, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(w) // newline after hidden input
	if err != nil {
		return "", err
	}
	return string(pw), nil
}
