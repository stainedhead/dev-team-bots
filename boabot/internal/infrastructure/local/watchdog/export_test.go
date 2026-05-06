package watchdog

import "runtime"

// SetReadMem replaces the readMem function on w for testing.
func SetReadMem(w *Watchdog, fn func(*runtime.MemStats)) {
	w.readMem = fn
}
