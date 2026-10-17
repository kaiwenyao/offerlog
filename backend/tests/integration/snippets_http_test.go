// 常用片段（snippets，migration 00008）的 HTTP 契约：增删改查、PATCH 只改
// 传了的字段、标题必填与长度校验、片段严格按用户隔离。
package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/identity/domain"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/httpx"
	snippetsrepo "offerlog/backend/internal/snippets"
	snippetstransport "offerlog/backend/internal/snippets/transport"
)

func newSnippetsServer(t *testing.T, db *database.DB, owner int64) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &domain.User{ID: owner, Email: "x@y.z", Timezone: "Europe/Dublin", Locale: "zh-CN"})
		c.Next()
	})
	snippetstransport.New(snippetsrepo.New(db)).Routes(r.Group("/api/v1/snippets"))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func snippetReq(t *testing.T, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestSnippetsCRUD(t *testing.T) {
	db, _, _, owner := setup(t)
	srv := newSnippetsServer(t, db, owner)
	base := srv.URL + "/api/v1/snippets"

	code, created := snippetReq(t, "POST", base,
		`{"title":"  字节实习  ","content":"- 负责推荐系统召回\n- QPS 提升 30%","category":" 实习经历 "}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %v", code, created)
	}
	if created["title"] != "字节实习" || created["category"] != "实习经历" {
		t.Fatalf("title/category not trimmed: %v", created)
	}
	if created["content"] != "- 负责推荐系统召回\n- QPS 提升 30%" {
		t.Fatalf("content must be kept verbatim: %q", created["content"])
	}
	id := itoa(int64(created["id"].(float64)))

	// 未分类的片段排在有分类的之后。
	if code, _ := snippetReq(t, "POST", base, `{"title":"Why us"}`); code != http.StatusCreated {
		t.Fatalf("create uncategorized = %d", code)
	}
	code, list := snippetReq(t, "GET", base, "")
	items, _ := list["items"].([]any)
	if code != http.StatusOK || len(items) != 2 {
		t.Fatalf("list = %d %v", code, list)
	}
	if items[0].(map[string]any)["category"] != "实习经历" || items[1].(map[string]any)["category"] != "" {
		t.Fatalf("list order: %v", items)
	}

	// PATCH 只改传了的字段。
	code, upd := snippetReq(t, "PATCH", base+"/"+id, `{"content":"新描述"}`)
	if code != http.StatusOK || upd["content"] != "新描述" || upd["title"] != "字节实习" || upd["category"] != "实习经历" {
		t.Fatalf("patch content = %d %v", code, upd)
	}
	// category:"" 显式清空 = 移到未分类。
	code, upd = snippetReq(t, "PATCH", base+"/"+id, `{"category":""}`)
	if code != http.StatusOK || upd["category"] != "" {
		t.Fatalf("patch clear category = %d %v", code, upd)
	}

	if code, _ := snippetReq(t, "DELETE", base+"/"+id, ""); code != http.StatusOK {
		t.Fatalf("delete = %d", code)
	}
	if code, _ := snippetReq(t, "DELETE", base+"/"+id, ""); code != http.StatusNotFound {
		t.Fatalf("delete twice = %d, want 404", code)
	}
}

func TestSnippetsValidation(t *testing.T) {
	db, _, _, owner := setup(t)
	srv := newSnippetsServer(t, db, owner)
	base := srv.URL + "/api/v1/snippets"

	for _, body := range []string{
		`{"content":"没有标题"}`,
		`{"title":"   "}`,
		`{"title":"` + strings.Repeat("长", 101) + `"}`,
		`{"title":"ok","category":"` + strings.Repeat("类", 41) + `"}`,
		`{"title":"ok","content":"` + strings.Repeat("x", 10001) + `"}`,
	} {
		if code, out := snippetReq(t, "POST", base, body); code != http.StatusBadRequest {
			t.Fatalf("POST %.40s… = %d %v, want 400", body, code, out)
		}
	}
	_, created := snippetReq(t, "POST", base, `{"title":"ok"}`)
	id := itoa(int64(created["id"].(float64)))
	if code, _ := snippetReq(t, "PATCH", base+"/"+id, `{"title":""}`); code != http.StatusBadRequest {
		t.Fatalf("PATCH empty title = %d, want 400", code)
	}
}

func TestSnippetsOwnerIsolation(t *testing.T) {
	db, _, _, owner := setup(t)
	other := createOwner(t, db)
	mine := newSnippetsServer(t, db, owner)
	theirs := newSnippetsServer(t, db, other)

	_, created := snippetReq(t, "POST", mine.URL+"/api/v1/snippets", `{"title":"私有片段"}`)
	id := itoa(int64(created["id"].(float64)))

	_, list := snippetReq(t, "GET", theirs.URL+"/api/v1/snippets", "")
	if items, _ := list["items"].([]any); len(items) != 0 {
		t.Fatalf("other user sees snippets: %v", items)
	}
	if code, _ := snippetReq(t, "PATCH", theirs.URL+"/api/v1/snippets/"+id, `{"title":"x"}`); code != http.StatusNotFound {
		t.Fatalf("cross-user PATCH = %d, want 404", code)
	}
	if code, _ := snippetReq(t, "DELETE", theirs.URL+"/api/v1/snippets/"+id, ""); code != http.StatusNotFound {
		t.Fatalf("cross-user DELETE = %d, want 404", code)
	}
}
