// Package screening defines the domain types and interfaces for content safety
// screening of untrusted input before it is forwarded to AI model providers.
package screening

const (
	untrustedOpen  = "<untrusted-content>"
	untrustedClose = "</untrusted-content>"
)

// ScreenResult is the output of a ContentScreener.Screen call.
type ScreenResult struct {
	// IsSafe indicates whether the screener found no disqualifying content.
	IsSafe bool

	// Findings is a list of human-readable descriptions of detected issues.
	// Empty when IsSafe is true.
	Findings []string

	// OriginalContent is the verbatim input passed to Screen.
	OriginalContent string

	// SanitisedContent is the content after any redaction or replacement
	// performed by the screener. Equals OriginalContent when no changes were
	// made.
	SanitisedContent string
}

// ContentScreener evaluates a string for safety and returns a ScreenResult.
type ContentScreener interface {
	// Screen analyses content and returns a ScreenResult describing whether the
	// content is safe and any sanitised version. Screen must never mutate the
	// original string.
	Screen(content string) (ScreenResult, error)
}

// WrapUntrusted wraps content in XML-style untrusted-content delimiters so
// that model system prompts can instruct the model to treat it with heightened
// scepticism.
func WrapUntrusted(content string) string {
	return untrustedOpen + content + untrustedClose
}
