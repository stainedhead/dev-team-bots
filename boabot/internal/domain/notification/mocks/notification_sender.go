// Package mocks provides hand-written test doubles for the notification domain
// interfaces.
package mocks

import "github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"

// SendCall records a single call to Send.
type SendCall struct {
	Notification notification.Notification
}

// NotificationSender is a hand-written mock of notification.NotificationSender.
type NotificationSender struct {
	SendFn    func(n notification.Notification) error
	SendCalls []SendCall
}

func (m *NotificationSender) Send(n notification.Notification) error {
	m.SendCalls = append(m.SendCalls, SendCall{Notification: n})
	if m.SendFn != nil {
		return m.SendFn(n)
	}
	return nil
}
