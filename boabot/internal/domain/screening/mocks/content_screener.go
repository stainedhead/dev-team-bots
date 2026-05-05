// Package mocks provides hand-written test doubles for the screening domain
// interfaces.
package mocks

import "github.com/stainedhead/dev-team-bots/boabot/internal/domain/screening"

// ScreenCall records a single call to Screen.
type ScreenCall struct {
	Content string
}

// ContentScreener is a hand-written mock of screening.ContentScreener.
type ContentScreener struct {
	ScreenFn    func(content string) (screening.ScreenResult, error)
	ScreenCalls []ScreenCall
}

func (m *ContentScreener) Screen(content string) (screening.ScreenResult, error) {
	m.ScreenCalls = append(m.ScreenCalls, ScreenCall{Content: content})
	if m.ScreenFn != nil {
		return m.ScreenFn(content)
	}
	return screening.ScreenResult{
		IsSafe:           true,
		OriginalContent:  content,
		SanitisedContent: content,
	}, nil
}
