// Package screening provides a rule-based content screener that detects common
// prompt injection patterns in external content before it is forwarded to a
// language model.
package screening

import (
	"regexp"
	"strings"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/screening"
)

// compiled once at package init for efficiency.
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore (previous|above|all) instructions?`),
	regexp.MustCompile(`(?i)(system|assistant|human):\s`),
	regexp.MustCompile(`(?i)you are now (a|an|the)`),
	regexp.MustCompile(`(?i)disregard (your|all|previous)`),
	regexp.MustCompile(`(?i)jailbreak`),
	regexp.MustCompile(`(?i)<\s*script[^>]*>`),
	regexp.MustCompile(`(?i)prompt (injection|override)`),
}

// RegexScreener implements domain/screening.ContentScreener using compiled
// regular expressions to detect common prompt injection signatures.
type RegexScreener struct{}

// NewRegexScreener constructs a RegexScreener.
func NewRegexScreener() *RegexScreener { return &RegexScreener{} }

// Screen checks content against all injection patterns. If any match, the
// result is marked unsafe, the matching strings are listed as findings, and
// the sanitised content has each match replaced with [REDACTED].
func (s *RegexScreener) Screen(content string) (screening.ScreenResult, error) {
	result := screening.ScreenResult{
		OriginalContent:  content,
		SanitisedContent: content,
		IsSafe:           true,
	}

	for _, pat := range injectionPatterns {
		matches := pat.FindAllString(content, -1)
		if len(matches) == 0 {
			continue
		}
		result.IsSafe = false
		result.Findings = append(result.Findings, matches...)
		result.SanitisedContent = pat.ReplaceAllString(result.SanitisedContent, "[REDACTED]")
	}

	// Deduplicate findings while preserving order.
	if len(result.Findings) > 0 {
		seen := make(map[string]struct{}, len(result.Findings))
		deduped := result.Findings[:0]
		for _, f := range result.Findings {
			k := strings.ToLower(f)
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				deduped = append(deduped, f)
			}
		}
		result.Findings = deduped
	}

	return result, nil
}
