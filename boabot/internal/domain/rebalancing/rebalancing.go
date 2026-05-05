// Package rebalancing defines the domain types and interfaces for detecting
// bottlenecks in the bot team and reassigning work items to unblock progress.
package rebalancing

import "github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"

// BotID is the unique identifier for a bot instance.
type BotID string

// WorkItemID is the unique identifier of a board work item.
type WorkItemID = workflow.WorkItemID

// BotStatus describes the current operational state of a single bot.
type BotStatus struct {
	// BotID is the unique bot identifier.
	BotID BotID

	// Role is the named role of this bot (e.g. "developer", "reviewer").
	Role string

	// QueueDepth is the number of pending tasks in the bot's queue.
	QueueDepth int

	// IsBlocked is true when the bot cannot make forward progress (e.g.
	// waiting on a dependency or external approval).
	IsBlocked bool

	// IsCapExceeded is true when the bot's budget or rate limit cap has been
	// exhausted.
	IsCapExceeded bool
}

// Bottleneck describes a detected blockage in the pipeline.
type Bottleneck struct {
	// BlockedBotID identifies the bot that is blocking progress.
	BlockedBotID string

	// Reason is a human-readable explanation of why the bottleneck was detected.
	Reason string

	// AffectedItems is the list of work item IDs that cannot proceed because of
	// this bottleneck.
	AffectedItems []WorkItemID
}

// Assignment describes a single rebalancing action: moving a work item from
// one bot to another.
type Assignment struct {
	// ItemID is the work item to be reassigned.
	ItemID WorkItemID

	// FromBotID is the bot currently holding the item.
	FromBotID BotID

	// ToBotID is the bot that should take ownership of the item.
	ToBotID BotID

	// Reason is a human-readable explanation for the reassignment.
	Reason string
}

// RebalancingEngine analyses the team state and produces rebalancing actions.
type RebalancingEngine interface {
	// DetectBottleneck inspects the current bot statuses and returns the most
	// critical bottleneck, or nil when no bottleneck is detected.
	DetectBottleneck(bots []BotStatus) *Bottleneck

	// Rebalance computes a set of Assignments to resolve the supplied
	// bottleneck. It returns an error when no valid rebalancing is possible.
	Rebalance(bottleneck Bottleneck) ([]Assignment, error)
}
