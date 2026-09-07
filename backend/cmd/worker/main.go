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

	// Reminder scheduler: a short ticker that enqueues one reminders pass per
	// UTC day (idempotency key "daily:<utc-day>"). A fixed 24h re-arm timer
	// would skip a whole day whenever the process sleeps/pauses across a
	// boundary; the ticker re-checks every 10 minutes and enqueues for the
	// current day the moment it is new, so a pause never loses a day. The
	// generator itself resolves "today" per user in their own zone and is
	// idempotent per occurrence, so enqueueing after a pause only produces the
	// notifications that day is due.
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		var lastKey string
		// fire once shortly after boot so a fresh worker still runs today's pass
		time.Sleep(10 * time.Second)
		for {
			day := time.Now().UTC().Format("2006-01-02")
			if day != lastKey {
				key := "daily:" + day
				if err := store.Enqueue(ctx, "reminders", key, map[string]any{}, time.Now()); err != nil {
					slog.Warn("enqueue reminders", "error", err)
				} else {
					lastKey = day
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
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
