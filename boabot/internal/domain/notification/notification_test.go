package notification_test

import (
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification/mocks"
)

func TestNotifType_Constants(t *testing.T) {
	types := []notification.NotifType{
		notification.NotifSuccess,
		notification.NotifFailure,
		notification.NotifBlocked,
		notification.NotifCostSpike,
		notification.NotifCostFlatCap,
		notification.NotifRateLimited,
		notification.NotifRebalanced,
	}
	for _, nt := range types {
		if nt == "" {
			t.Fatalf("empty NotifType found in constants")
		}
	}
}

func TestNotification_Fields(t *testing.T) {
	n := notification.Notification{
		Type:         notification.NotifCostSpike,
		RecipientARN: "arn:aws:sns:us-east-1:123456789012:alerts",
		Subject:      "Cost spike detected",
		Body:         "Bot bot-1 exceeded 30% of daily cap",
		Metadata:     map[string]string{"bot_id": "bot-1", "spend": "3.50"},
	}
	if n.Type != notification.NotifCostSpike {
		t.Fatalf("unexpected Type %s", n.Type)
	}
	if n.Metadata["bot_id"] != "bot-1" {
		t.Fatalf("unexpected metadata bot_id %s", n.Metadata["bot_id"])
	}
}

func TestNotificationSenderMock_Send_OK(t *testing.T) {
	m := &mocks.NotificationSender{}
	err := m.Send(notification.Notification{
		Type:    notification.NotifSuccess,
		Subject: "Done",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.SendCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.SendCalls))
	}
	if m.SendCalls[0].Notification.Subject != "Done" {
		t.Fatalf("unexpected subject %s", m.SendCalls[0].Notification.Subject)
	}
}

func TestNotificationSenderMock_Send_Error(t *testing.T) {
	sentinel := errors.New("sns unavailable")
	m := &mocks.NotificationSender{
		SendFn: func(_ notification.Notification) error { return sentinel },
	}
	err := m.Send(notification.Notification{Type: notification.NotifFailure})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error got %v", err)
	}
}

func TestNotificationSenderMock_Send_MultipleCalls(t *testing.T) {
	m := &mocks.NotificationSender{}
	notifs := []notification.Notification{
		{Type: notification.NotifBlocked, Subject: "First"},
		{Type: notification.NotifRateLimited, Subject: "Second"},
		{Type: notification.NotifRebalanced, Subject: "Third"},
	}
	for _, n := range notifs {
		if err := m.Send(n); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if len(m.SendCalls) != 3 {
		t.Fatalf("expected 3 calls got %d", len(m.SendCalls))
	}
}
