package webhook

import (
	"context"
	"testing"

	"github.com/matryer/is"
)

// TestFiredEventHandler_RegistrationFiresAfterEachEvent verifies that
// once a handler is registered with SetFiredEventHandler it is
// invoked once per dispatched event with the original payload. This
// is the seam the CI subsystem (rule CreateRunsOnEvent in ci.allium)
// hooks into to fan webhook events out into pending Runs.
func TestFiredEventHandler_RegistrationFiresAfterEachEvent(t *testing.T) {
	is := is.New(t)

	t.Cleanup(func() { ClearFiredEventHandler() })

	var seen []EventPayload
	SetFiredEventHandler(func(_ context.Context, payload EventPayload) {
		seen = append(seen, payload)
	})

	payload := stubPayload{event: EventPush, repoID: 7, repoName: "repo7"}
	dispatchFiredEvent(context.Background(), payload)
	dispatchFiredEvent(context.Background(), payload)

	is.Equal(len(seen), 2) // handler invoked once per dispatch
	is.Equal(seen[0].Event(), EventPush)
	is.Equal(seen[0].RepositoryName(), "repo7")
}

// TestFiredEventHandler_NotRegistered_NoOp ensures dispatchFiredEvent
// is safe to call when no handler has been registered.
func TestFiredEventHandler_NotRegistered_NoOp(t *testing.T) {
	t.Cleanup(func() { ClearFiredEventHandler() })

	ClearFiredEventHandler()
	dispatchFiredEvent(context.Background(), stubPayload{event: EventPush, repoID: 1, repoName: "x"})
}

// TestFiredEventHandler_PanicInHandlerDoesNotFailDispatch verifies
// that a misbehaving subscriber cannot break the webhook fan-out
// path; SendEvent's job is to deliver webhooks, the subscriber is a
// best-effort follower.
func TestFiredEventHandler_PanicInHandlerDoesNotFailDispatch(t *testing.T) {
	t.Cleanup(func() { ClearFiredEventHandler() })

	SetFiredEventHandler(func(_ context.Context, _ EventPayload) {
		panic("kaboom")
	})

	dispatchFiredEvent(context.Background(), stubPayload{event: EventPush, repoID: 1, repoName: "x"})
	// If we get here without the panic propagating, the recover works.
}

type stubPayload struct {
	event    Event
	repoID   int64
	repoName string
}

func (p stubPayload) Event() Event             { return p.event }
func (p stubPayload) RepositoryID() int64      { return p.repoID }
func (p stubPayload) RepositoryName() string   { return p.repoName }
