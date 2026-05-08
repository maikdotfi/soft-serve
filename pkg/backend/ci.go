package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/soft-serve/git"
	"github.com/charmbracelet/soft-serve/pkg/ci"
	"github.com/charmbracelet/soft-serve/pkg/hooks"
	"github.com/charmbracelet/soft-serve/pkg/webhook"
)

// CIPreReceive implements the RepoPushGate surface from ci.allium.
// For each ref being created or updated to a non-zero SHA, it
// validates the magic folder under .soft-serve/workflows/ in the
// incoming tree by asking the CI service to parse it. If any tree
// fails to parse, the parse error is returned and a human-readable
// message is written to stderr so the git client surfaces it on
// rejection.
//
// When the CI service is not configured (SetCIService has not been
// called) the gate is a no-op, matching the behavior of the backup
// service guard in PostReceive.
func (b *Backend) CIPreReceive(ctx context.Context, repo string, args []hooks.HookArg, stderr io.Writer) error {
	if b.ci == nil {
		return nil
	}

	for _, arg := range args {
		// Skip ref deletions — they have no incoming tree.
		if git.IsZeroHash(arg.NewSha) {
			continue
		}
		// Only validate updates to branch heads. Tag refs (refs/tags/)
		// don't carry workflow files we care about, and arbitrary
		// refs (refs/notes, refs/changes, refs/pull) are out of scope
		// for the gate.
		if !strings.HasPrefix(arg.RefName, "refs/heads/") {
			continue
		}
		if err := b.ci.ValidateWorkflowsAtCommit(ctx, repo, arg.NewSha); err != nil {
			if errors.Is(err, ci.ErrWorkflowParse) {
				fmt.Fprintf(stderr, "soft-serve: rejecting push: %s in %s\n", err, arg.RefName)
			} else {
				fmt.Fprintf(stderr, "soft-serve: rejecting push: failed to validate workflows in %s: %s\n", arg.RefName, err)
			}
			return err
		}
	}
	return nil
}

// CIPostReceive implements the post-acceptance half of the
// WorkflowsSyncedOnPush rule: with the new ref now active, the
// stored Workflow set for the repo is reconciled with the parsed
// contents of the magic folder at HEAD.
//
// The gate has already validated parseability at pre-receive time,
// so a parse error here is unexpected (it would imply a race or
// concurrent change between pre-receive and post-receive). It is
// logged but not propagated: the push has already been accepted.
func (b *Backend) CIPostReceive(ctx context.Context, repo string, args []hooks.HookArg) {
	if b.ci == nil {
		return
	}
	// Only sync if at least one head ref was updated; ref deletions
	// or tag-only pushes don't change the workflow source of truth.
	if !hasHeadUpdate(args) {
		return
	}
	if err := b.ci.SyncWorkflowsOnPush(ctx, repo); err != nil {
		b.logger.Error("ci: sync workflows on push failed", "repo", repo, "err", err)
	}
}

// OnWebhookFired is the in-process webhook subscriber that the
// server registers via webhook.SetFiredEventHandler. For each
// dispatched event it invokes ci.Service.HandleWebhookEvent so the
// CreateRunsOnEvent rule (ci.allium) can fan the event out into
// pending Runs.
//
// The signature matches webhook.FiredEventHandler so the server
// startup can wire it directly: webhook.SetFiredEventHandler(be.OnWebhookFired).
func (b *Backend) OnWebhookFired(ctx context.Context, payload webhook.EventPayload) {
	if b.ci == nil {
		return
	}
	eventType, ok := webhookEventToCI(payload.Event())
	if !ok {
		return
	}
	if err := b.ci.HandleWebhookEvent(ctx, payload.RepositoryName(), eventType); err != nil {
		b.logger.Error("ci: handle webhook event failed",
			"repo", payload.RepositoryName(),
			"event", payload.Event(),
			"err", err)
	}
}

// webhookEventToCI maps the webhook package's Event taxonomy to the
// EventType enum declared in ci.allium. Returning ok=false means the
// CI domain does not recognise this event and the subscriber should
// skip it.
func webhookEventToCI(e webhook.Event) (ci.EventType, bool) {
	switch e {
	case webhook.EventPush:
		return ci.EventTypePush, true
	case webhook.EventBranchTagCreate:
		return ci.EventTypeBranchTagCreate, true
	case webhook.EventBranchTagDelete:
		return ci.EventTypeBranchTagDelete, true
	case webhook.EventCollaborator:
		return ci.EventTypeCollaborator, true
	case webhook.EventRepository:
		return ci.EventTypeRepository, true
	case webhook.EventRepositoryVisibilityChange:
		return ci.EventTypeRepositoryVisibilityChange, true
	default:
		return "", false
	}
}

func hasHeadUpdate(args []hooks.HookArg) bool {
	for _, arg := range args {
		if git.IsZeroHash(arg.NewSha) {
			continue
		}
		if strings.HasPrefix(arg.RefName, "refs/heads/") {
			return true
		}
	}
	return false
}
