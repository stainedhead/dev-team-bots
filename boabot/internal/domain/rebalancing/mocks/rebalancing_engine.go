// Package mocks provides hand-written test doubles for the rebalancing domain
// interfaces.
package mocks

import "github.com/stainedhead/dev-team-bots/boabot/internal/domain/rebalancing"

// DetectBottleneckCall records a single call to DetectBottleneck.
type DetectBottleneckCall struct {
	Bots []rebalancing.BotStatus
}

// RebalanceCall records a single call to Rebalance.
type RebalanceCall struct {
	Bottleneck rebalancing.Bottleneck
}

// RebalancingEngine is a hand-written mock of rebalancing.RebalancingEngine.
type RebalancingEngine struct {
	DetectBottleneckFn func(bots []rebalancing.BotStatus) *rebalancing.Bottleneck
	RebalanceFn        func(bottleneck rebalancing.Bottleneck) ([]rebalancing.Assignment, error)

	DetectBottleneckCalls []DetectBottleneckCall
	RebalanceCalls        []RebalanceCall
}

func (m *RebalancingEngine) DetectBottleneck(bots []rebalancing.BotStatus) *rebalancing.Bottleneck {
	m.DetectBottleneckCalls = append(m.DetectBottleneckCalls, DetectBottleneckCall{Bots: bots})
	if m.DetectBottleneckFn != nil {
		return m.DetectBottleneckFn(bots)
	}
	return nil
}

func (m *RebalancingEngine) Rebalance(bottleneck rebalancing.Bottleneck) ([]rebalancing.Assignment, error) {
	m.RebalanceCalls = append(m.RebalanceCalls, RebalanceCall{Bottleneck: bottleneck})
	if m.RebalanceFn != nil {
		return m.RebalanceFn(bottleneck)
	}
	return nil, nil
}
