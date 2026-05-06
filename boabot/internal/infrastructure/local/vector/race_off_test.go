//go:build !race

package vector_test

import "time"

// searchTimeBudget is the allowed Search duration for the 100k-vector test.
// Without the race detector the search completes in ~40ms on modern hardware.
const searchTimeBudget = 100 * time.Millisecond
