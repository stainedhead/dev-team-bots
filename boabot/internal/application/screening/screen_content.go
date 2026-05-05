// Package screening contains the application-layer use case for content safety
// screening.
package screening

import (
	"fmt"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/screening"
)

// ScreenContentUseCase screens untrusted content and wraps the result in
// untrusted-content delimiters before returning it to callers.
type ScreenContentUseCase struct {
	screener screening.ContentScreener
}

// NewScreenContentUseCase constructs a ScreenContentUseCase.
func NewScreenContentUseCase(screener screening.ContentScreener) *ScreenContentUseCase {
	return &ScreenContentUseCase{screener: screener}
}

// Screen analyses content for safety. If the content is unsafe the sanitised
// version is used. The returned string is always wrapped in untrusted-content
// delimiters. An error is returned only when the screener itself fails.
func (u *ScreenContentUseCase) Screen(content string) (string, error) {
	result, err := u.screener.Screen(content)
	if err != nil {
		return "", fmt.Errorf("screen content: %w", err)
	}

	effective := result.OriginalContent
	if !result.IsSafe {
		effective = result.SanitisedContent
	}

	return screening.WrapUntrusted(effective), nil
}
