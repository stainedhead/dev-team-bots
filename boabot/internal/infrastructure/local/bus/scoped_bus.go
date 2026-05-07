package bus

// NewScopedBus returns a new *Bus that is structurally isolated from any other
// Bus instance. Two ScopedBus values share no subscriber state. Each call
// returns a fresh bus with its own internal subscriber map, so broadcasts on
// one instance never reach subscribers registered on the other.
func NewScopedBus() *Bus { return New() }
