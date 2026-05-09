//go:build race

package vector_test

import "time"

// searchTimeBudget is the allowed Search duration for the 100k-vector test.
// The race detector adds ~10–15x overhead to memory-access instrumentation;
// 10s provides headroom for slower CI runners.
const searchTimeBudget = 10 * time.Second
