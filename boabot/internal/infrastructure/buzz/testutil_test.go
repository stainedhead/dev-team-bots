package buzz

import (
	"io"
	"log/slog"
)

// discardLogger returns a logger that writes nowhere, for tests that need a
// non-nil *slog.Logger but don't assert on its output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
