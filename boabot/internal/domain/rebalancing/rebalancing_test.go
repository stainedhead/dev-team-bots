package rebalancing_test

import (
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/rebalancing"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/rebalancing/mocks"
)

func TestBotStatus_Fields(t *testing.T) {
	bs := rebalancing.BotStatus{
		BotID:         "bot-1",
		Role:          "developer",
		QueueDepth:    5,
		IsBlocked:     true,
		IsCapExceeded: false,
	}
	if bs.BotID != "bot-1" {
		t.Fatalf("unexpected BotID %s", bs.BotID)
	}
	if bs.QueueDepth != 5 {
		t.Fatalf("unexpected QueueDepth %d", bs.QueueDepth)
	}
	if !bs.IsBlocked {
		t.Fatal("expected IsBlocked=true")
	}
}

func TestBottleneck_Fields(t *testing.T) {
	b := rebalancing.Bottleneck{
		BlockedBotID:  "bot-2",
		Reason:        "cap exceeded",
		AffectedItems: []rebalancing.WorkItemID{"item-1", "item-2"},
	}
	if b.BlockedBotID != "bot-2" {
		t.Fatalf("unexpected BlockedBotID %s", b.BlockedBotID)
	}
	if len(b.AffectedItems) != 2 {
		t.Fatalf("expected 2 affected items got %d", len(b.AffectedItems))
	}
}

func TestAssignment_Fields(t *testing.T) {
	a := rebalancing.Assignment{
		ItemID:    "item-3",
		FromBotID: "bot-old",
		ToBotID:   "bot-new",
		Reason:    "load balancing",
	}
	if a.ItemID != "item-3" {
		t.Fatalf("unexpected ItemID %s", a.ItemID)
	}
	if a.Reason != "load balancing" {
		t.Fatalf("unexpected Reason %s", a.Reason)
	}
}

func TestRebalancingEngineMock_DetectBottleneck_None(t *testing.T) {
	m := &mocks.RebalancingEngine{}
	result := m.DetectBottleneck([]rebalancing.BotStatus{
		{BotID: "bot-1", QueueDepth: 0},
	})
	if result != nil {
		t.Fatalf("expected nil bottleneck got %+v", result)
	}
	if len(m.DetectBottleneckCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.DetectBottleneckCalls))
	}
}

func TestRebalancingEngineMock_DetectBottleneck_Found(t *testing.T) {
	bn := &rebalancing.Bottleneck{BlockedBotID: "bot-heavy", Reason: "queue full"}
	m := &mocks.RebalancingEngine{
		DetectBottleneckFn: func(bots []rebalancing.BotStatus) *rebalancing.Bottleneck {
			for _, b := range bots {
				if b.QueueDepth > 10 {
					return bn
				}
			}
			return nil
		},
	}
	result := m.DetectBottleneck([]rebalancing.BotStatus{
		{BotID: "bot-heavy", QueueDepth: 15},
		{BotID: "bot-light", QueueDepth: 1},
	})
	if result == nil {
		t.Fatal("expected bottleneck to be detected")
	}
	if result.BlockedBotID != "bot-heavy" {
		t.Fatalf("unexpected BlockedBotID %s", result.BlockedBotID)
	}
}

func TestRebalancingEngineMock_Rebalance_OK(t *testing.T) {
	assignments := []rebalancing.Assignment{
		{ItemID: "item-1", FromBotID: "bot-old", ToBotID: "bot-new"},
	}
	m := &mocks.RebalancingEngine{
		RebalanceFn: func(_ rebalancing.Bottleneck) ([]rebalancing.Assignment, error) {
			return assignments, nil
		},
	}
	got, err := m.Rebalance(rebalancing.Bottleneck{BlockedBotID: "bot-old"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 assignment got %d", len(got))
	}
	if len(m.RebalanceCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.RebalanceCalls))
	}
}

func TestRebalancingEngineMock_Rebalance_Error(t *testing.T) {
	sentinel := errors.New("no available bots")
	m := &mocks.RebalancingEngine{
		RebalanceFn: func(_ rebalancing.Bottleneck) ([]rebalancing.Assignment, error) {
			return nil, sentinel
		},
	}
	_, err := m.Rebalance(rebalancing.Bottleneck{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error got %v", err)
	}
}
