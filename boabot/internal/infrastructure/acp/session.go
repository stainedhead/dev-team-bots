package acp

import "context"

// session tracks one ACP session's in-flight turn, so a concurrent
// session/cancel notification can stop it (architecture.md's Data Flow
// step 7).
type session struct {
	cancel context.CancelFunc
}
