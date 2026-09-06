// Command api runs the OfferLog HTTP API (frontend static files + /api/v1).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"offerlog/backend/internal/bootstrap"
	"offerlog/backend/internal/platform/config"
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

	// Run migrations once before serving (single migrating process).
	if err := migrate.Up(ctx, app.DB.Pool()); err != nil {
		slog.Error("migrate", "error", err)
		os.Exit(1)
	}

	// Serve static frontend if a dist directory exists next to the binary or
	// under ./frontend/dist.
	srv := &http.Server{Addr: cfg.HTTP.Addr, Handler: staticHandler(app, cfg)}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	slog.Info("api listening", "addr", cfg.HTTP.Addr)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		slog.Info("shutting down")
		shutCtx := context.Background()
		_ = srv.Shutdown(shutCtx)
	}
}

func staticHandler(app *bootstrap.App, cfg config.Config) http.Handler {
	apiHandler := app.Handler()
	exeDir, _ := os.Executable()
	exeParent := filepath.Dir(filepath.Dir(exeDir))
	candidates := []string{
		"/srv",
		filepath.Join(exeParent, "frontend", "dist"),
		filepath.Join(cfg.App.DataDir, "..", "frontend", "dist"),
		"../frontend/dist",
		"./frontend/dist",
		"./dist",
	}
	var dist string
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			dist = c
			break
		}
	}
	if dist == "" {
		// API-only mode (dev).
		return apiHandler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			apiHandler.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			apiHandler.ServeHTTP(w, r)
			return
		}
		p := filepath.Join(dist, filepath.Clean(r.URL.Path))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			http.ServeFile(w, r, p)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html")) // SPA fallback
	})
}
