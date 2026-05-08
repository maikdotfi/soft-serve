// Package jobs cron-job for the CI subsystem. Three discrete passes
// run on every tick:
//
//   - DispatchPendingRun for each pending Run (rule DispatchRun).
//   - EnforceTimeouts (rule PickupTimeout).
//   - RotateExpiredRuns (rule RotateExpiredRuns).
//
// All three are idempotent and cheap when the system is idle, so a
// single short interval is sufficient. The interval is intentionally
// not exposed as user config in v1 — the spec doesn't require it
// and it keeps the cron table small.
package jobs

import (
	"context"

	"charm.land/log/v2"
	"github.com/charmbracelet/soft-serve/pkg/backend"
	"github.com/charmbracelet/soft-serve/pkg/ci"
)

func init() {
	Register("ci", ciJob{})
}

// ciJob runs the CI reconciliation passes on a fixed cadence.
type ciJob struct{}

// Spec returns the cron spec for the CI job. The job is registered
// unconditionally; if no CI service is wired the Func body is a
// no-op, so a missing/disabled CI subsystem costs at most a wakeup
// per minute.
func (ciJob) Spec(_ context.Context) string {
	return "@every 1m"
}

// Func returns the function that runs each tick.
func (ciJob) Func(ctx context.Context) func() {
	logger := log.FromContext(ctx).WithPrefix("jobs.ci")
	return func() {
		be := backend.FromContext(ctx)
		if be == nil {
			return
		}
		svc := be.CIService()
		if svc == nil {
			return
		}

		dispatchPendingRuns(ctx, svc, logger)

		if err := svc.EnforceTimeouts(ctx); err != nil {
			logger.Error("ci: enforce timeouts failed", "err", err)
		}

		if err := svc.RotateExpiredRuns(ctx); err != nil {
			logger.Error("ci: rotate expired runs failed", "err", err)
		}
	}
}

// dispatchPendingRuns walks the run list and asks the service to
// advance each pending one. A failure on a single run is logged and
// does not stop the rest of the pass; the next tick will retry.
func dispatchPendingRuns(ctx context.Context, svc *ci.Service, logger *log.Logger) {
	runs, err := svc.ListPendingRuns(ctx)
	if err != nil {
		logger.Error("ci: list pending runs failed", "err", err)
		return
	}
	for _, run := range runs {
		if err := svc.DispatchPendingRun(ctx, run.ID); err != nil {
			logger.Error("ci: dispatch pending run failed", "run", run.ID, "err", err)
		}
	}
}
