// 岗位基础信息编辑（PATCH /applications/:id）的回归测试。
//
// 用户反馈：岗位创建时只填了「公司 + 岗位」，之后公司/岗位/城市/JD 链接填错了
// 没有编辑入口，薪资、渠道、截止日期也补不上——而创建弹窗却承诺「保存后仍可继续
// 编辑完整信息」。修复后 PATCH 必须能补齐并改错这些字段，尤其是原先根本没进
// patchReq 的薪资三件套（salary_min/max/currency），同时保持乐观锁语义：
// version 不匹配返回 409，而不是覆盖别人的改动。
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// patchApp sends a PATCH /applications/:id body and decodes the JSON response.
func patchApp(t *testing.T, srv *httptest.Server, id int64, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/applications/"+itoa(id), bytes.NewBufferString(body))
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

func TestPatchApplicationFillsAndFixesBaseInfo(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "EditCo", "Backend")

	srv := newAppDetailServer(t, db, owner, svc)
	defer srv.Close()

	// 创建时只要求公司和岗位；这里把城市/JD 链接/渠道/截止日期/备注与薪资一起补齐。
	status, out := patchApp(t, srv, app.ID, `{
		"version": 1,
		"company_name": "EditCo Renamed",
		"position": "Senior Backend",
		"location": "上海",
		"job_url": "https://editco.test/jd",
		"channel": "内推",
		"deadline": "2026-10-01",
		"salary_min": 25000,
		"salary_max": 35000,
		"salary_currency": "CNY",
		"notes": "改错后补齐"
	}`)
	if status != http.StatusOK {
		t.Fatalf("PATCH base info -> %d body=%v", status, out)
	}
	// 乐观锁：每次成功更新 version 递增，前端拿到的就是新值。
	if out["version"] != float64(2) {
		t.Fatalf("version after patch = %v, want 2", out["version"])
	}
	if out["company_name"] != "EditCo Renamed" || out["position"] != "Senior Backend" {
		t.Fatalf("rename not applied: %v / %v", out["company_name"], out["position"])
	}
	// 薪资必须真的落库（原先 patchReq 根本没有这三个字段，PATCH 会被静默忽略）。
	var min, max *int64
	var currency, location, jobURL, channel, notes string
	var deadline *string
	if err := db.Pool().QueryRow(ctx, `SELECT salary_min, salary_max, salary_currency,
		location, job_url, channel, notes, to_char(deadline,'YYYY-MM-DD')
		FROM applications WHERE id=$1`, app.ID).Scan(
		&min, &max, &currency, &location, &jobURL, &channel, &notes, &deadline); err != nil {
		t.Fatal(err)
	}
	if min == nil || *min != 25000 || max == nil || *max != 35000 || currency != "CNY" {
		t.Fatalf("salary not persisted: min=%v max=%v currency=%q", min, max, currency)
	}
	if location != "上海" || jobURL != "https://editco.test/jd" || channel != "内推" || notes != "改错后补齐" {
		t.Fatalf("base info not persisted: %q %q %q %q", location, jobURL, channel, notes)
	}
	if deadline == nil || *deadline != "2026-10-01" {
		t.Fatalf("deadline = %v, want 2026-10-01", deadline)
	}

	// 详情读回的值与 PATCH 响应一致（前端改完刷新详情就能看到）。
	_, detail := getBody(t, srv, "/api/v1/applications/"+itoa(app.ID))
	if detail["salary_min"] != float64(25000) || detail["salary_currency"] != "CNY" {
		t.Fatalf("detail salary mismatch: %v %v", detail["salary_min"], detail["salary_currency"])
	}
}

func TestPatchApplicationClearsSalaryAndDeadline(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "ClearCo", "Role")
	srv := newAppDetailServer(t, db, owner, svc)
	defer srv.Close()

	// 先填上薪资和截止日期…
	if status, out := patchApp(t, srv, app.ID, `{"version":1,"salary_min":20000,"salary_max":30000,"salary_currency":"EUR","deadline":"2026-11-01"}`); status != http.StatusOK {
		t.Fatalf("seed salary -> %d body=%v", status, out)
	}
	// …再显式清空：null 表示「删掉」，空字符串清除截止日期。填错了必须能删。
	status, out := patchApp(t, srv, app.ID, `{"version":2,"salary_min":null,"salary_max":null,"salary_currency":"","deadline":""}`)
	if status != http.StatusOK {
		t.Fatalf("clear salary -> %d body=%v", status, out)
	}
	var min, max *int64
	var currency string
	var deadline *string
	if err := db.Pool().QueryRow(ctx, `SELECT salary_min, salary_max, salary_currency, to_char(deadline,'YYYY-MM-DD')
		FROM applications WHERE id=$1`, app.ID).Scan(&min, &max, &currency, &deadline); err != nil {
		t.Fatal(err)
	}
	if min != nil || max != nil || currency != "" || deadline != nil {
		t.Fatalf("clear failed: min=%v max=%v currency=%q deadline=%v", min, max, currency, deadline)
	}
}

func TestPatchApplicationRejectsBadSalary(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "BadSalaryCo", "Role")
	srv := newAppDetailServer(t, db, owner, svc)
	defer srv.Close()

	// 下限高于上限：422（domain.ValidationError 的统一映射），且库里不能留下自相矛盾的薪资。
	if status, out := patchApp(t, srv, app.ID, `{"version":1,"salary_min":40000,"salary_max":30000}`); status != http.StatusUnprocessableEntity {
		t.Fatalf("inverted salary -> %d body=%v, want 422", status, out)
	}
	// 负数同样拒绝。
	if status, _ := patchApp(t, srv, app.ID, `{"version":1,"salary_min":-1,"salary_max":100}`); status != http.StatusUnprocessableEntity {
		t.Fatalf("negative salary -> %d, want 422", status)
	}
	// 非数字不是整数：400（不是静默忽略）。
	if status, _ := patchApp(t, srv, app.ID, `{"version":1,"salary_min":"abc"}`); status != http.StatusBadRequest {
		t.Fatalf("non-numeric salary -> %d, want 400", status)
	}
	var min, max *int64
	if err := db.Pool().QueryRow(ctx, `SELECT salary_min, salary_max FROM applications WHERE id=$1`, app.ID).Scan(&min, &max); err != nil {
		t.Fatal(err)
	}
	if min != nil || max != nil {
		t.Fatalf("rejected patch must not persist: min=%v max=%v", min, max)
	}
}

func TestPatchApplicationVersionConflict(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "LockCo", "Role")
	srv := newAppDetailServer(t, db, owner, svc)
	defer srv.Close()

	if status, out := patchApp(t, srv, app.ID, `{"version":1,"location":"柏林"}`); status != http.StatusOK {
		t.Fatalf("first patch -> %d body=%v", status, out)
	}
	// 陈旧 version（另一个标签页已改过）必须 409，而不是覆盖。
	status, out := patchApp(t, srv, app.ID, `{"version":1,"location":"东京"}`)
	if status != http.StatusConflict {
		t.Fatalf("stale version -> %d body=%v, want 409", status, out)
	}
	var location string
	if err := db.Pool().QueryRow(ctx, `SELECT location FROM applications WHERE id=$1`, app.ID).Scan(&location); err != nil {
		t.Fatal(err)
	}
	if location != "柏林" {
		t.Fatalf("conflict must not overwrite: location=%q", location)
	}
}
