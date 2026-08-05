package buzz

// Phase I (tasks.md I3): dispatch-latency measurement harness. Exercises
// the real Monitor.handleChannelEvent (relay-delivery -> queue-enqueue)
// and Monitor.HandleResult (task-result -> reply-publish) paths under
// concurrent load, against Phase F's existing fakeRelay/mocks.MessageQueue
// seams -- no live relay required, per the PRD AC ("measurement harness is
// committed with the tests"). The two targets asserted are the PRD's own:
// relay-delivery -> enqueue p95 < 500ms, HandleResult -> publish p95 < 1s.
//
// Both call paths are synchronous up to (and including) the operation
// being measured -- dispatch() calls m.queue.Send inline before returning,
// and HandleResult calls m.relay.Publish inline before returning -- so
// wall-clock-around-the-call is a faithful measurement of exactly the
// PRD's two named legs, not of any downstream async work (F16's typing
// loop, F13's logging) that happens after the measured boundary.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// latencyHarness accumulates duration samples from concurrent goroutines
// and reports percentiles. Not specific to Buzz -- a small, generic
// measurement tool -- but lives here since I3 is this package's own task.
type latencyHarness struct {
	mu      sync.Mutex
	samples []time.Duration
}

func (h *latencyHarness) record(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.samples = append(h.samples, d)
}

// percentile returns the p-th percentile (0..100) of recorded samples,
// using nearest-rank on the sorted sample set. Panics if no samples were
// recorded -- a harness bug, not a runtime condition callers need to
// handle.
func (h *latencyHarness) percentile(p float64) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.samples) == 0 {
		panic("latencyHarness: percentile called with no recorded samples")
	}
	sorted := make([]time.Duration, len(h.samples))
	copy(sorted, h.samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(p/100*float64(len(sorted))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func (h *latencyHarness) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.samples)
}

// runConcurrent calls fn n times split across concurrency goroutines,
// waiting for all to finish -- this is the "measured under load" half of
// the PRD AC, not a single-shot timing.
func runConcurrent(n, concurrency int, fn func(i int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

const (
	latencySampleCount        = 500
	latencyConcurrency        = 32
	dispatchEnqueueP95Target  = 500 * time.Millisecond
	handleResultPublishTarget = 1 * time.Second
)

// TestDispatchLatency_RelayDeliveryToEnqueue_P95UnderTarget measures
// Monitor.handleChannelEvent's relay-delivery-simulated-as-a-function-call
// -> domain.MessageQueue.Send leg under concurrent load. Target: p95 <
// 500ms (PRD line 560).
func TestDispatchLatency_RelayDeliveryToEnqueue_P95UnderTarget(t *testing.T) {
	fr := newFakeRelay()
	q := &mocks.MessageQueue{}
	m := newTestMonitor(fr, q, nil)

	h := &latencyHarness{}
	runConcurrent(latencySampleCount, latencyConcurrency, func(i int) {
		evt := domain.Event{
			ID:      uuid.New().String(),
			PubKey:  fmt.Sprintf("human-%d", i),
			Kind:    9,
			Tags:    [][]string{{"h", "chan-1"}, {"p", "self-pk"}},
			Content: fmt.Sprintf("ping %d", i),
		}
		start := time.Now()
		m.handleChannelEvent(context.Background(), "chan-1", evt)
		h.record(time.Since(start))
	})

	if got := h.count(); got != latencySampleCount {
		t.Fatalf("recorded %d samples, want %d", got, latencySampleCount)
	}
	p95 := h.percentile(95)
	t.Logf("relay-delivery->enqueue: n=%d p50=%v p95=%v p99=%v target(p95)=%v",
		h.count(), h.percentile(50), p95, h.percentile(99), dispatchEnqueueP95Target)
	if p95 >= dispatchEnqueueP95Target {
		t.Fatalf("relay-delivery->enqueue p95 = %v, want < %v", p95, dispatchEnqueueP95Target)
	}
}

// TestDispatchLatency_HandleResultToPublish_P95UnderTarget measures
// Monitor.HandleResult's task-result -> relay Publish leg under concurrent
// load. Target: p95 < 1s (PRD line 560). Each sample first dispatches a
// distinct task (so HandleResult has a real pending-map entry to resolve
// against, exactly as F12 requires) and only times the HandleResult call
// itself.
func TestDispatchLatency_HandleResultToPublish_P95UnderTarget(t *testing.T) {
	fr := newFakeRelay()

	// contentToTaskID correlates each goroutine's uniquely-worded mention
	// with the taskID dispatch() minted for it (uuid.New(), unpredictable
	// ahead of time) -- populated synchronously inside Send, which Monitor
	// calls inline before dispatch() returns, so no data race with the
	// lookup below.
	var contentToTaskID sync.Map
	q := &mocks.MessageQueue{
		SendFn: func(_ context.Context, _ string, msg domain.Message) error {
			var payload domain.TaskPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				return err
			}
			contentToTaskID.Store(payload.Instruction, payload.TaskID)
			return nil
		},
	}
	m := newTestMonitor(fr, q, nil)

	h := &latencyHarness{}
	runConcurrent(latencySampleCount, latencyConcurrency, func(i int) {
		content := fmt.Sprintf("ping %d", i)
		evt := domain.Event{
			ID:      uuid.New().String(),
			PubKey:  fmt.Sprintf("human-%d", i),
			Kind:    9,
			Tags:    [][]string{{"h", "chan-1"}, {"p", "self-pk"}},
			Content: content,
		}
		m.handleChannelEvent(context.Background(), "chan-1", evt)

		var taskID string
		waitFor(t, time.Second, func() bool {
			v, ok := contentToTaskID.Load(content)
			if !ok {
				return false
			}
			taskID = v.(string)
			return true
		})

		start := time.Now()
		m.HandleResult(context.Background(), domain.TaskResultPayload{
			TaskID:  taskID,
			Success: true,
			Output:  "pong",
		})
		h.record(time.Since(start))
	})

	if got := h.count(); got != latencySampleCount {
		t.Fatalf("recorded %d samples, want %d", got, latencySampleCount)
	}
	p95 := h.percentile(95)
	t.Logf("HandleResult->publish: n=%d p50=%v p95=%v p99=%v target(p95)=%v",
		h.count(), h.percentile(50), p95, h.percentile(99), handleResultPublishTarget)
	if p95 >= handleResultPublishTarget {
		t.Fatalf("HandleResult->publish p95 = %v, want < %v", p95, handleResultPublishTarget)
	}
}
