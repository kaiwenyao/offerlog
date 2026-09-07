// Command worker runs background jobs: file cleanup, reminder generation and
// snapshot expiry. It leases work from the jobs table; safe to run several
// replicas (postgres leases + SKIP LOCKED). Unknown job kinds are recorded as
// failures (never silently succeeded); handlers run under a lease that expires
// after a crash so another replica can reclaim the job (§4.3).
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
	"offerlog/backend/internal/reminders"
	"offerlog/backend/internal/worker"
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
	reg := worker.NewRegistry()
	gen := reminders.New(app.DB)

	// reminder generation runs on a daily schedule; each pass inserts
	// idempotent in-app notifications per user preference.
	reg.Register("reminders", func(ctx context.Context, store worker.Store, job *jobs.Job) error {
		now := time.Now()
		n, err := gen.Run(ctx, now)
		if err != nil {
			return err
		}
		slog.Info("reminder pass", "inserted", n)
		return nil
	})
	// file cleanup registered by the worker ticker below (not a queued job yet).
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

	// Daily enqueue at 08:00 user-local is approximated: the worker enqueues a
	// reminder pass on boot and after each pass re-enqueues 24h later (a real
	// scheduler would compute the next 08:00 per user; in-app notifications are
	// generated for "today" whenever the pass runs). The idempotency key is
	// per-day so a completed pass never blocks the next one (the jobs unique
	// index is on kind+key and would otherwise make the enqueue a permanent
	// no-op after the first run).
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				day := time.Now().UTC().Format("2006-01-02")
				if err := store.Enqueue(ctx, "reminders", "daily:"+day, map[string]any{}, time.Now()); err != nil {
					slog.Warn("enqueue reminders", "error", err)
				}
				timer.Reset(24 * time.Hour)
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
		if err := reg.Process(ctx, store, job); err != nil {
			slog.Warn("job processing error", "kind", job.Kind, "id", job.ID, "error", err)
		}
	}
}
