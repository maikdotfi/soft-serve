package webhook

import (
	"context"
	"sync/atomic"
)

// FiredEventHandler is a callback invoked after a webhook event has
// been dispatched to subscribed external URLs. It is intended for
// in-process subscribers that need to react to the same fan-out —
// notably the CI subsystem, which translates webhook events into
// pending Runs (rule CreateRunsOnEvent in ci.allium).
//
// Handlers MUST treat their argument as read-only. Errors raised by
// a handler do not affect webhook dispatch; any panic is recovered
// so a misbehaving subscriber cannot break the dispatch path.
type FiredEventHandler func(ctx context.Context, payload EventPayload)

// firedEventHandler holds the (optional) single registered handler.
// atomic.Pointer keeps reads on the dispatch path lock-free.
var firedEventHandler atomic.Pointer[FiredEventHandler]

// SetFiredEventHandler registers (or replaces) the post-dispatch
// hook. There is at most one handler; later calls overwrite earlier
// ones. Pass nil to ClearFiredEventHandler instead.
func SetFiredEventHandler(h FiredEventHandler) {
	firedEventHandler.Store(&h)
}

// ClearFiredEventHandler removes any previously registered handler.
// Subsequent dispatches will not invoke a follower until another
// handler is registered.
func ClearFiredEventHandler() {
	firedEventHandler.Store(nil)
}

// dispatchFiredEvent invokes the registered handler, if any, with
// the given payload. Panics from the handler are recovered and
// dropped; this function must be safe to call from SendEvent.
func dispatchFiredEvent(ctx context.Context, payload EventPayload) {
	hp := firedEventHandler.Load()
	if hp == nil || *hp == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	(*hp)(ctx, payload)
}
