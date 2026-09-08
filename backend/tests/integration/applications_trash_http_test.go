// Regression（回收站详情 404）: 数据库页回收站里单击一行会打开与普通记录相同的
// 详情抽屉，但 GET /applications/:id 走的是 includeDeleted=false 的读取，软删除行
// 返回 404，抽屉渲染「加载失败」——而 events / interviews / notes 等子资源接口
// 对同一个 id 照常 200（线上现象即：只有 events 请求成功返回）。修复后按 id 直读
// 必须连同回收站行一起返回（响应携带 deleted=true，驱动抽屉 RowMenu 的「恢复」
// 入口）；可见性过滤仍由列表接口负责，越权读取依旧 404。
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	appservice "offerlog/backend/internal/applications/service"
	apptransport "offerlog/backend/internal/applications/transport"
	iddomain "offerlog/backend/internal/identity/domain"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/httpx"
)

// newAppDetailServer mounts the applications routes acting as the given owner
// (same wiring style as newFullActivityServer; CSRF belongs to the parent
// router in production and is irrelevant for GETs).
func newAppDetailServer(t *testing.T, db *database.DB, owner int64, svc *appservice.Service) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &iddomain.User{ID: owner, Email: "x@y.z", Timezone: "Europe/Dublin", Locale: "zh-CN"})
		c.Next()
	})
	h := apptransport.New(svc)
	h.Routes(r.Group("/api/v1/applications"))
	return httptest.NewServer(r)
}

func getBody(t *testing.T, srv *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	res, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res.StatusCode, body
}

func TestGetApplicationServesTrashedRowToItsOwner(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "TrashCo", "TrashRole")
	if err := svc.SoftDelete(ctx, owner, app.ID); err != nil {
		t.Fatal(err)
	}

	srv := newAppDetailServer(t, db, owner, svc)
	defer srv.Close()

	// 属主打开回收站记录：详情 200 且 deleted=true，抽屉可正常加载。
	status, body := getBody(t, srv, "/api/v1/applications/"+itoa(app.ID))
	if status != http.StatusOK {
		t.Fatalf("GET trashed application -> %d, want 200", status)
	}
	if body["deleted"] != true {
		t.Fatalf("trashed application deleted flag = %v, want true", body["deleted"])
	}
	if body["company_name"] != "TrashCo" {
		t.Fatalf("trashed application company = %v, want TrashCo", body["company_name"])
	}

	// 子资源（events）与详情必须一致可用，否则抽屉仍只拿到半截数据。
	if status, _ := getBody(t, srv, "/api/v1/applications/"+itoa(app.ID)+"/events"); status != http.StatusOK {
		t.Fatalf("GET trashed application events -> %d, want 200", status)
	}

	// 恢复后 deleted 回到 false。
	if err := svc.Restore(ctx, owner, app.ID); err != nil {
		t.Fatal(err)
	}
	if status, body := getBody(t, srv, "/api/v1/applications/"+itoa(app.ID)); status != http.StatusOK || body["deleted"] != false {
		t.Fatalf("GET restored application -> %d deleted=%v, want 200/false", status, body["deleted"])
	}
}

func TestGetApplicationStillScopesToOwner(t *testing.T) {
	db, svc, _, ownerA := setup(t)
	ownerB := createOwner(t, db)
	appA := mustCreate(t, svc, ownerA, "VictimCo", "SecretRole")
	if err := svc.SoftDelete(context.Background(), ownerA, appA.ID); err != nil {
		t.Fatal(err)
	}

	// includeDeleted 放宽的只是可见性，不是归属：他人读软删除行仍然 404。
	srvB := newAppDetailServer(t, db, ownerB, svc)
	defer srvB.Close()
	if status, _ := getBody(t, srvB, "/api/v1/applications/"+itoa(appA.ID)); status != http.StatusNotFound {
		t.Fatalf("foreign GET trashed application -> %d, want 404", status)
	}
	if status, _ := getBody(t, srvB, "/api/v1/applications/999999999"); status != http.StatusNotFound {
		t.Fatalf("GET nonexistent application -> %d, want 404", status)
	}
}
