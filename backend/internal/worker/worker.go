// Package worker implements the background job processor with explicit
// per-kind handlers. Unknown kinds FAIL rather than silently succeeding, and
// every handler is executed with a lease that is renewed while running; a
// crashed worker therefore has its lease expire and another replica re-claims
// the job (idempotency keys prevent duplicate side effects).
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

// Store is the jobs surface the registry needs — success/failure recording.
// *jobs.Store satisfies it; tests use an in-memory double.
type Store interface {
	Succeed(ctx context.Context, id int64) error
	Fail(ctx context.Context, id int64, attempts, maxAttempts int, msg string, backoff time.Duration) error
}

// Registry maps job kinds to handlers. `timeout` bounds a single execution so
// the lease cannot starve other jobs; `backoff` schedules retries on failure.
type Registry struct {
	handlers map[string]Handler
	timeout  time.Duration
	backoff  time.Duration
}

func NewRegistry() *Registry {
	return &Registry{handlers: map[string]Handler{}, timeout: 5 * time.Minute, backoff: 30 * time.Second}
}

// Register binds a kind to its handler.
func (r *Registry) Register(kind string, h Handler) { r.handlers[kind] = h }

// Process runs a claimed job: executes the handler for its kind, then marks
// success or failure with the appropriate status. Unknown kinds are recorded
// as terminal failures (never a false success).
func (r *Registry) Process(ctx context.Context, store Store, job *jobs.Job) error {
	h, ok := r.handlers[job.Kind]
	if !ok {
		// Unknown kind: record a terminal failure. The job remains visible to
		// the operator via `api-admin jobs`; it is never silently succeeded.
		msg := fmt.Sprintf("未知任务类型 %q", job.Kind)
		slog.Error("job unknown kind", "id", job.ID, "kind", job.Kind)
		if err := store.Fail(ctx, job.ID, job.Attempts, job.MaxAttempts, msg, 0); err != nil {
			return err
		}
		return nil
	}

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
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
		return store.Fail(ctx, job.ID, job.Attempts, job.MaxAttempts, msg, r.backoff)
	}
	slog.Info("job done", "id", job.ID, "kind", job.Kind, "elapsed_ms", time.Since(start).Milliseconds())
	return store.Succeed(ctx, job.ID)
}
