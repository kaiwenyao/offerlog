// Package worker implements the background job processor with explicit
// per-kind handlers. Unknown kinds FAIL rather than silently succeeding, and
// every handler runs under a lease that the worker RENEWS while executing; a
// crashed worker therefore has its lease expire and another replica re-claims
// the job, and completion (Succeed/Fail) is fenced on the claimed lease so a
// stale replica can never overwrite the new owner's result. Handlers still
// must be idempotent for the crash window between lease expiry and reclaim.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"offerlog/backend/internal/platform/jobs"
)

// Handler executes one job kind. It must be idempotent: a lease expiry may
// cause the same job to run twice after a crash.
type Handler func(ctx context.Context, store Store, job *jobs.Job) error

// Store is the jobs surface the registry needs — lease renewal and fenced
// success/failure recording. *jobs.Store satisfies it; tests use a double.
type Store interface {
	RenewLease(ctx context.Context, id int64, leaseSeconds int, claimed *time.Time) error
	Succeed(ctx context.Context, id int64, claimed *time.Time) error
	Fail(ctx context.Context, id int64, attempts, maxAttempts int, msg string, backoff time.Duration, claimed *time.Time) error
}

// Registry maps job kinds to handlers.
type Registry struct {
	handlers map[string]Handler
	timeout  time.Duration
	backoff  time.Duration
	// leaseSeconds / renewEvery control the DB lease: the worker claims with
	// leaseSeconds and renews every renewEvery, so a handler that outlives the
	// original lease keeps the job owned. Handler timeout must stay below the
	// lease so a hung handler is reclaimed promptly after its lease expires.
	leaseSeconds int
	renewEvery   time.Duration
}

func NewRegistry() *Registry {
	return &Registry{
		handlers:     map[string]Handler{},
		timeout:      5 * time.Minute,
		backoff:      30 * time.Second,
		leaseSeconds: 60,
		renewEvery:   20 * time.Second,
	}
}

// Register binds a kind to its handler.
func (r *Registry) Register(kind string, h Handler) { r.handlers[kind] = h }

// WithLease overrides the DB lease parameters (used by tests / tuning).
func (r *Registry) WithLease(leaseSeconds int, renewEvery time.Duration) *Registry {
	r.leaseSeconds = leaseSeconds
	r.renewEvery = renewEvery
	return r
}

// Process runs a claimed job: renews the lease while the handler executes,
// then marks success or failure fenced on the claimed lease. Unknown kinds
// are recorded as terminal failures (never a false success).
func (r *Registry) Process(ctx context.Context, store Store, job *jobs.Job) error {
	claimed := job.LeaseUntil

	h, ok := r.handlers[job.Kind]
	if !ok {
		// Unknown kind: record a terminal failure. The job remains visible to
		// the operator via `api-admin jobs`; it is never silently succeeded.
		msg := fmt.Sprintf("未知任务类型 %q", job.Kind)
		slog.Error("job unknown kind", "id", job.ID, "kind", job.Kind)
		return store.Fail(ctx, job.ID, job.Attempts, job.MaxAttempts, msg, 0, claimed)
	}

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Lease renewal: keep the job owned while it runs. Renewal failures are
	// logged and stop the loop — if we lost the lease, the handler result must
	// not be written (fencing in Succeed/Fail enforces that anyway).
	renewStop := make(chan struct{})
	go func() {
		t := time.NewTicker(r.renewEvery)
		defer t.Stop()
		for {
			select {
			case <-renewStop:
				return
			case <-t.C:
				if err := store.RenewLease(ctx, job.ID, r.leaseSeconds, claimed); err != nil {
					slog.Warn("lease renew failed", "id", job.ID, "kind", job.Kind, "error", err)
					return
				}
			}
		}
	}()
	defer close(renewStop)

	start := time.Now()
	jobErr := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		return h(runCtx, store, job)
	}()

	if jobErr != nil {
		msg := jobErr.Error()
		slog.Warn("job failed", "id", job.ID, "kind", job.Kind, "elapsed_ms", time.Since(start).Milliseconds(), "error", msg)
		return store.Fail(ctx, job.ID, job.Attempts, job.MaxAttempts, msg, r.backoff, claimed)
	}
	if err := store.Succeed(ctx, job.ID, claimed); err != nil {
		slog.Warn("job done but lease lost", "id", job.ID, "kind", job.Kind, "error", err)
		return err
	}
	slog.Info("job done", "id", job.ID, "kind", job.Kind, "elapsed_ms", time.Since(start).Milliseconds())
	return nil
}
