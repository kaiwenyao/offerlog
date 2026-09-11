// 路由接线回归（review: PR #23 把 /health/live 吞进过注释行）。
//
// bootstrap.Handler() 组装了整个 HTTP 面，但没有任何测试直接打它，所以一条
// 被注释吞掉的 handler（/health/live 404，main.go 却还在为它做特殊转发）
// 全绿地进了 PR。这里用真实配置组装一次 handler，把免认证的健康检查和
// 一个受保护的 meta 端点都打一遍。
package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/bootstrap"
	"offerlog/backend/internal/platform/config"
)

func TestBootstrapHandlerWiresHealthAndMetaRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// config.Load() reads env; the integration suite's DATABASE_URL (or the
	// dev default) points at the real test database bootstrap.New migrates on.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.Database.URL = dbURL(t)
	// Keep the test hermetic: a throwaway local object store dir.
	cfg.ObjectStore.Provider = "local"
	cfg.ObjectStore.LocalDir = t.TempDir()
	cfg.App.CSRFSecret = "test-only-secret"

	app, err := bootstrap.New(t.Context(), cfg)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	defer app.DB.Close()
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	// /health/live must answer without auth — main.go special-cases it for
	// k8s liveness probes, so a missing handler reads as a crash-looping pod.
	res, err := http.Get(srv.URL + "/health/live")
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/health/live = %d, want 200", res.StatusCode)
	}
	res, err = http.Get(srv.URL + "/health/ready")
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/health/ready = %d, want 200", res.StatusCode)
	}

	// /api/v1/meta/status-model must exist and be auth-gated (401, not 404):
	// the whole point of the route is that the client loads the whitelist.
	res, err = http.Get(srv.URL + "/api/v1/meta/status-model")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/api/v1/meta/status-model = %d, want 401 (exists but auth-gated)", res.StatusCode)
	}
}
