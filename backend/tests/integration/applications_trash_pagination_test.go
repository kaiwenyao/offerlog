// 回归：回收站必须真的分页。
//
// 原始 bug：前端把回收站请求写死成 page=1（后端 list 本来就支持 trash=1&page），
// 于是点「下一页」只改页码不换数据——超过 60 条删除记录时，第 2 页的条目既翻不到
// 也恢复不了。这个测试从 HTTP 层锁住「page 真的生效、total 与全量一致」：
// 只要 list 把 page 忽略掉（回到写死行为），第 2 页就会重新吐出第 1 页的内容。
package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func listTrash(t *testing.T, srv *httptest.Server, page, size int) ([]any, float64) {
	t.Helper()
	status, body := getBody(t, srv, "/api/v1/applications?trash=1&page="+itoa(int64(page))+"&page_size="+itoa(int64(size)))
	if status != http.StatusOK {
		t.Fatalf("trash page=%d -> %d, want 200", page, status)
	}
	items, _ := body["items"].([]any)
	total, _ := body["total"].(float64)
	return items, total
}

func TestTrashListPaginates(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()

	// 3 条删除记录，page_size=2 → 第 1 页 2 条、第 2 页 1 条，total 始终是 3。
	for _, name := range []string{"回收A", "回收B", "回收C"} {
		app := mustCreate(t, svc, owner, name, "R")
		if err := svc.SoftDelete(ctx, owner, app.ID); err != nil {
			t.Fatal(err)
		}
	}

	srv := newAppDetailServer(t, db, owner, svc)
	defer srv.Close()

	p1, total := listTrash(t, srv, 1, 2)
	if len(p1) != 2 || total != 3 {
		t.Fatalf("第 1 页 = %d 条, total=%v; want 2/3", len(p1), total)
	}
	p2, total2 := listTrash(t, srv, 2, 2)
	if len(p2) != 1 || total2 != 3 {
		t.Fatalf("第 2 页 = %d 条, total=%v; want 1/3", len(p2), total2)
	}

	// 两页必须是不相交的记录集合——写死 page=1 时这里会拿到同一批行。
	seen := map[float64]bool{}
	for _, it := range p1 {
		seen[it.(map[string]any)["id"].(float64)] = true
	}
	for _, it := range p2 {
		id := it.(map[string]any)["id"].(float64)
		if seen[id] {
			t.Fatalf("第 2 页重复了第 1 页的记录 id=%v（page 没有生效）", id)
		}
	}

	// 第 2 页的记录同样可以恢复。
	second := p2[0].(map[string]any)["id"].(float64)
	if _, err := http.Post(srv.URL+"/api/v1/applications/"+itoa(int64(second))+"/restore", "application/json", nil); err != nil {
		t.Fatal(err)
	}
	if _, total := listTrash(t, srv, 1, 2); total != 2 {
		t.Fatalf("恢复第 2 页的记录后回收站 total=%v, want 2", total)
	}
}

// 没有删除记录时回收站是空的，而不是报错或退化成全量列表。
func TestTrashListEmpty(t *testing.T) {
	db, svc, _, owner := setup(t)
	mustCreate(t, svc, owner, "没删除", "R")

	srv := newAppDetailServer(t, db, owner, svc)
	defer srv.Close()
	if items, total := listTrash(t, srv, 1, 60); len(items) != 0 || total != 0 {
		t.Fatalf("回收站应为空, got %d 条 total=%v", len(items), total)
	}
}
