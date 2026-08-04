package buzz

import "time"

// ticker abstracts *time.Ticker so the presence (F14) and typing-indicator
// (F16) loops are unit-testable without real timers -- mirrors
// reconnect.go's injectable WithSleep/WithJitter seams.
type ticker interface {
	c() <-chan time.Time
	stop()
}

type realTicker struct{ t *time.Ticker }

func (r *realTicker) c() <-chan time.Time { return r.t.C }
func (r *realTicker) stop()               { r.t.Stop() }

// newRealTicker is the production tickerFunc, wrapping time.NewTicker.
func newRealTicker(d time.Duration) ticker { return &realTicker{t: time.NewTicker(d)} }
