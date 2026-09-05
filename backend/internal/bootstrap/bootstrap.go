// Package bootstrap wires dependencies explicitly and starts/stops the HTTP
// API and the worker. No global mutable connections.
package bootstrap

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	actrepo "offerlogs/backend/internal/activities/repository"
	acttransport "offerlogs/backend/internal/activities/transport"
	antrepo "offerlogs/backend/internal/analytics"
	anttransport "offerlogs/backend/internal/analytics/transport"
	apprepo "offerlogs/backend/internal/applications/repository"
	appservice "offerlogs/backend/internal/applications/service"
	apptransport "offerlogs/backend/internal/applications/transport"
	filetransport "offerlogs/backend/internal/files/transport"
	idrepo "offerlogs/backend/internal/identity/repository"
	idservice "offerlogs/backend/internal/identity/service"
	idtransport "offerlogs/backend/internal/identity/transport"
	"offerlogs/backend/internal/platform/config"
	"offerlogs/backend/internal/platform/database"
	"offerlogs/backend/internal/platform/httpx"
	"offerlogs/backend/internal/platform/jobs"
	"offerlogs/backend/internal/platform/objectstore"
	trrepo "offerlogs/backend/internal/transfers"
	trtransport "offerlogs/backend/internal/transfers/transport"
	vrepo "offerlogs/backend/internal/views/repository"
	vservice "offerlogs/backend/internal/views/service"
	vtransport "offerlogs/backend/internal/views/transport"
)

// App bundles the running services and their dependencies.
type App struct {
	Cfg   config.Config
	DB    *database.DB
	Store objectstore.Store
	Auth  *idservice.Store
	Jobs  *jobs.Store

	appsSvc  *appservice.Service
	viewsSvc *vservice.Service
}

// New builds the dependency graph and applies migrations.
func New(ctx context.Context, cfg config.Config) (*App, error) {
	db, err := database.New(ctx, cfg.Database.URL)
	if err != nil {
		return nil, err
	}
	obj, err := objectstore.New(objectstore.Config{
		Provider: cfg.ObjectStore.Provider, LocalDir: cfg.ObjectStore.LocalDir,
		Endpoint: cfg.ObjectStore.Endpoint, Region: cfg.ObjectStore.Region,
		Bucket: cfg.ObjectStore.Bucket, AccessKey: cfg.ObjectStore.AccessKey,
		SecretKey: cfg.ObjectStore.SecretKey, UsePathStyle: cfg.ObjectStore.UsePathStyle,
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	users := idrepo.NewSQLUsers(db)
	auth := idservice.New(db, users, cfg.HTTP.SessionHours, cfg.App.CSRFSecret)

	appsRepo := apprepo.New(db)
	appsSvc := appservice.New(db, appsRepo)
	viewsRepo := vrepo.New(db)
	viewsSvc := vservice.New(db, viewsRepo)
	jobStore := jobs.NewStore(db)

	return &App{
		Cfg: cfg, DB: db, Store: obj, Auth: auth, Jobs: jobStore,
		appsSvc: appsSvc, viewsSvc: viewsSvc,
	}, nil
}

// Handler builds the full gin engine with all routes mounted.
func (a *App) Handler() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), httpx.RequestID(), httpx.SecurityHeaders(), httpx.Auth(a.Auth))

	api := r.Group("/api/v1", httpx.CSRF(a.Auth))
	// Auth endpoints live OUTSIDE the CSRF-protected group: login has no
	// session yet (nothing to protect), and logout only clears the cookie.
	// A dedicated Origin check middleware guards them.
	authH := idtransport.New(a.Auth, a.Cfg.HTTP.PublicBase != "" && strings.HasPrefix(a.Cfg.HTTP.PublicBase, "https"))
	authRoutes := api.Group("/auth")
	authH.Routes(authRoutes)

	// applications
	appH := apptransport.New(a.appsSvc)
	appH.Routes(api.Group("/applications"))

	// activities under /applications/:id
	actRepo := actrepo.New(a.DB)
	actH := acttransport.New(actRepo)
	actH.Routes(api.Group("/applications/:id"))
	// standalone actions list for “今日待办” (all applications)
	actH.ActionsRoot(api.Group("/actions"))

	// files
	fileH := filetransport.New(a.Store, a.DB, filetransport.Config{
		MaxFileBytes: a.Cfg.ObjectStore.MaxFileBytes, QuotaBytes: a.Cfg.ObjectStore.QuotaBytes,
	})
	fileH.Routes(api.Group("/files"))

	// views / properties / rich query
	viewH := vtransport.New(a.viewsSvc)
	viewH.Routes(api.Group("/views"))
	viewH.PropertiesRoutes(api.Group("/properties"))

	// analytics
	antRepo := antrepo.New(a.DB)
	antH := anttransport.New(antRepo, a.DB)
	antH.Routes(api.Group("/analytics"))

	// transfers
	trRepo := trrepo.New(a.DB)
	trH := trtransport.New(trRepo)
	trH.Routes(api.Group("/imports"))
	trH.ExportRoutes(api.Group("/exports"))

	// health (no auth)
	r.GET("/health/live", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/health/ready", a.ready)

	// SPA fallback handled by the static file server in cmd/api.
	return r
}

func (a *App) ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := a.DB.Pool().Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "reason": "database"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// RunAPI starts the HTTP server and blocks until ctx is cancelled.
func (a *App) RunAPI(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: a.Handler()}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
