// Command worker runs background jobs: file cleanup, pending reminders,
// snapshot expiry. It leases work from the jobs table; safe to run several
// replicas (postgres leases + SKIP LOCKED).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"offerlog/backend/internal/bootstrap"
	"offerlog/backend/internal/platform/config"
	"offerlog/backend/internal/platform/jobs"
	"offerlog/backend/internal/platform/migrate"
	"offerlog/backend/internal/platform/observability"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}
	observability.Init(cfg.App.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := bootstrap.New(ctx, cfg)
	if err != nil {
		slog.Error("bootstrap", "error", err)
		os.Exit(1)
	}
	defer app.DB.Close()
	if err := migrate.Up(ctx, app.DB.Pool()); err != nil {
		slog.Error("migrate", "error", err)
		os.Exit(1)
	}

	store := app.Jobs
	// periodic housekeeping ticker
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n, err := store.CleanupStalePendings(ctx, cfg.ObjectStore.GracePeriod); err == nil && n > 0 {
					slog.Info("cleaned stale pending files", "count", n)
				}
			}
		}
	}()

	slog.Info("worker started")
	for {
		job, err := store.Claim(ctx, 60)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("claim", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if job == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		if err := process(ctx, app, store, job); err != nil {
			slog.Warn("job failed", "kind", job.Kind, "id", job.ID, "error", err)
		}
	}
}

func process(ctx context.Context, app *bootstrap.App, store *jobs.Store, job *jobs.Job) error {
	// v1 jobs: no long-running tasks are enqueued by normal flows yet (imports
	// run synchronously). This loop is the foundation for future exports and
	// async file deletion.
	switch job.Kind {
	case "ping":
		slog.Info("ping job", "id", job.ID)
	default:
		slog.Info("unknown job kind", "kind", job.Kind, "id", job.ID)
	}
	return store.Succeed(ctx, job.ID)
}
