// 三个「改完 / 删完之后另一处还留着旧东西」的回归，都属于同一个家族：本地只改
// 一份数据，而界面上同一件事有三四个入口。
//
//  1. 删掉最后一条待办不能让岗位行上的 next_action 镜像「复活」成首页待办。
//  2. 老待办（带着迁移 00002 回填的 due_ts）改截止日必须真的生效。
//  3. 删掉 / 改期一轮 OA 必须清掉「OA 明天截止」通知。
package integration

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	actrepo "offerlog/backend/internal/activities/repository"
	acttransport "offerlog/backend/internal/activities/transport"
	apprepo "offerlog/backend/internal/applications/repository"
	"offerlog/backend/internal/home"
	identitydomain "offerlog/backend/internal/identity/domain"
	"offerlog/backend/internal/notifications"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/prefs"
	"offerlog/backend/internal/reminders"
)

// activityServerFull wires the app-scoped activities routes AND the standalone
// /actions root with BOTH the progress mirror and the notification store,
// exactly like bootstrap does. The reminder tests need both: the mirror writes
// the application substatus, and the store is what the new clearing hooks reach
// for.
func activityServerFull(t *testing.T, db *database.DB, owner int64) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &identitydomain.User{ID: owner, Email: "x@y.z", Timezone: "Europe/Dublin", Locale: "zh-CN"})
		c.Next()
	})
	h := acttransport.New(actrepo.New(db)).
		WithApplicationSync(apprepo.New(db)).
		WithNotifications(notifications.New(db))
	api := r.Group("/api/v1")
	h.Routes(api.Group("/applications/:id"))
	// 待办的删除 / 完成 / 延期走独立根（前端 tabs.tsx 就是这么调的）
	h.ActionsRoot(api.Group("/actions"))
	return httptest.NewServer(r)
}

// openTodos returns the unified-todo count the home badge shows plus how many of
// those rows are legacy-derived (action_id IS NULL) — the shape a resurrected
// mirror takes, and exactly the rows that cannot be completed or deleted.
func openTodos(t *testing.T, db *database.DB, owner int64) (open int, derived int) {
	t.Helper()
	ctx := context.Background()
	var tz string
	if err := db.Pool().QueryRow(ctx, `SELECT timezone FROM users WHERE id=$1`, owner).Scan(&tz); err != nil {
		t.Fatalf("load tz: %v", err)
	}
	s, err := home.New(db).Get(ctx, owner, tz, time.Monday, time.Now(), 20)
	if err != nil {
		t.Fatalf("home summary: %v", err)
	}
	for _, it := range s.TodoItems {
		if it.ActionID == nil {
			derived++
		}
	}
	return int(s.Todos.Open), derived
}

func doJSON(t *testing.T, method, url, body string) int {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode
}

// 删掉唯一那条待办时，岗位行上的 next_action 镜像必须一起退休。
//
// 首页统一待办的 derived 分支守卫是「有 next_action 且该岗位一条 actions 行都
// 没有」——只删行不清镜像，守卫就失效，那条早被独立待办取代的旧文案会以
// action_id=null 的形式回到今日待办和侧栏计数里，而且只有「查看」：完不成、也删
// 不掉。详情页同时又会多出一条「旧记录（迁移后并入统一待办）」。
func TestDeletingLastActionRetiresLegacyMirror(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "MirrorCo", "后端工程师")

	// 真实链路：ActionForm 建完待办会把标题镜像写进岗位行。
	var actionID int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, done_at, remind_me, priority, source)
		VALUES($1,$2,'准备二面', CURRENT_DATE + 1, NULL, FALSE, 'medium','manual') RETURNING id`,
		app.ID, owner).Scan(&actionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET next_action='准备二面', next_action_due_at=CURRENT_DATE + 1 WHERE id=$1`,
		app.ID); err != nil {
		t.Fatal(err)
	}

	// 镜像与待办并存时首页只算一条：独立待办是唯一真相。
	if open, derived := openTodos(t, db, owner); open != 1 || derived != 0 {
		t.Fatalf("before delete: open=%d derived=%d, want 1/0", open, derived)
	}

	srv := activityServerFull(t, db, owner)
	defer srv.Close()
	if status := doJSON(t, "DELETE", srv.URL+"/api/v1/actions/"+itoa(actionID), ""); status != http.StatusOK {
		t.Fatalf("delete action -> %d, want 200", status)
	}

	// 关键断言：不能复活。
	open, derived := openTodos(t, db, owner)
	if open != 0 || derived != 0 {
		t.Errorf("after delete: open=%d derived=%d, want 0/0 — the legacy next_action resurrected as an un-completable row", open, derived)
	}
	// 机制层面：镜像被清空（详情页也就不会再挂出「旧记录」）。
	var nextAction string
	var dueAt *time.Time
	if err := db.Pool().QueryRow(ctx, `SELECT COALESCE(next_action,''), next_action_due_at FROM applications WHERE id=$1`,
		app.ID).Scan(&nextAction, &dueAt); err != nil {
		t.Fatal(err)
	}
	if nextAction != "" || dueAt != nil {
		t.Errorf("mirror = %q / %v, want empty and nil", nextAction, dueAt)
	}
}

// 还有别的待办（未完成或已完成）时，镜像不能被顺手清掉：守卫仍然有效，清掉反而
// 会让「下一步」列空掉。
func TestDeletingOneOfTwoActionsKeepsTheOtherIntact(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "TwoActions", "前端工程师")

	var first, second int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, done_at, remind_me, priority, source)
		VALUES($1,$2,'第一条', CURRENT_DATE + 1, NULL, FALSE, 'medium','manual') RETURNING id`,
		app.ID, owner).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, done_at, remind_me, priority, source)
		VALUES($1,$2,'第二条', CURRENT_DATE + 2, NULL, FALSE, 'medium','manual') RETURNING id`,
		app.ID, owner).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET next_action='第二条', next_action_due_at=CURRENT_DATE + 2 WHERE id=$1`,
		app.ID); err != nil {
		t.Fatal(err)
	}

	srv := activityServerFull(t, db, owner)
	defer srv.Close()
	if status := doJSON(t, "DELETE", srv.URL+"/api/v1/actions/"+itoa(first), ""); status != http.StatusOK {
		t.Fatalf("delete action -> %d, want 200", status)
	}

	if open, derived := openTodos(t, db, owner); open != 1 || derived != 0 {
		t.Errorf("open=%d derived=%d, want 1/0 (the remaining action is still the todo)", open, derived)
	}
	var nextAction string
	if err := db.Pool().QueryRow(ctx, `SELECT COALESCE(next_action,'') FROM applications WHERE id=$1`, app.ID).Scan(&nextAction); err != nil {
		t.Fatal(err)
	}
	if nextAction != "第二条" {
		t.Errorf("mirror = %q, want 第二条 (an action row still exists, so nothing to retire)", nextAction)
	}
}

// 老待办带着迁移 00002 回填的 due_ts；所有读取端都是 due_ts 优先，所以前端改
// 截止日时必须把 due_ts 一起清掉，否则显示 / 日历 / 提醒纹丝不动。
//
// 这条覆盖后端那半边：整体覆盖的 PATCH 确实能清掉 due_ts（前端送 null 即可）。
func TestActionPatchCanClearLegacyDueTs(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "DueTsCo", "数据工程师")

	var actionID int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, due_ts, done_at, remind_me, priority, source)
		VALUES($1,$2,'老待办', CURRENT_DATE + 1, now() + interval '1 day', NULL, FALSE, 'medium','manual') RETURNING id`,
		app.ID, owner).Scan(&actionID); err != nil {
		t.Fatal(err)
	}

	srv := newActivityServer(t, db, owner)
	defer srv.Close()

	// 前端 buildActionPatch 在「用户动了日期」时就是发这个形状。
	body := `{"title":"老待办","due_date":"2026-12-24","due_ts":null,"done_at":null,"remind_me":false,"priority":"medium"}`
	if status := doJSON(t, "PATCH", srv.URL+"/api/v1/actions/"+itoa(actionID), body); status != http.StatusOK {
		t.Fatalf("patch action -> %d, want 200", status)
	}

	var dueTs *time.Time
	var dueDate *time.Time
	if err := db.Pool().QueryRow(ctx, `SELECT due_ts, due_date FROM actions WHERE id=$1`, actionID).Scan(&dueTs, &dueDate); err != nil {
		t.Fatal(err)
	}
	if dueTs != nil {
		t.Errorf("due_ts = %v, want nil — 旧的精确时间会盖住新选的日历日，改了等于没改", dueTs)
	}
	if dueDate == nil || dueDate.Format("2006-01-02") != "2026-12-24" {
		t.Errorf("due_date = %v, want 2026-12-24", dueDate)
	}
}

// 删掉一轮 OA 之后，「OA 明天截止」通知必须一起消失：否则通知中心挂着一条指向
// 已经不存在轮次的提醒。（改期同理：幂等键 assessment_due:<id>:<day> 只按日期去重，
// 旧键不清会把新一轮的提醒一起静音。）
func TestAssessmentRemindersClearedOnDeleteAndReschedule(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "OARemindCo", "测试工程师")

	// 面试 / OA 前一天提醒受「流程提醒」开关控制。
	_ = prefs.New(db).Upsert(ctx, &prefs.Preferences{
		UserID: owner, Timezone: "Europe/Dublin", WeekStart: 1,
		RemindOverdue: false, RemindInterview: true, RemindStaleDays: 0,
	})

	insertRound := func(due time.Time) int64 {
		var id int64
		if err := db.Pool().QueryRow(ctx, `INSERT INTO assessment_rounds(application_id, owner_id, kind, name, progress, result, due_at)
			VALUES($1,$2,'online_test','OA','preparing','unknown',$3) RETURNING id`, app.ID, owner, due).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	countDueNotices := func() int {
		var n int
		if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM notifications
			WHERE owner_id=$1 AND kind='assessment_due'`, owner).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	tomorrow := time.Now().AddDate(0, 0, 1)
	roundID := insertRound(tomorrow)
	if _, err := reminders.New(db).Run(ctx, time.Now()); err != nil {
		t.Fatalf("reminders: %v", err)
	}
	if countDueNotices() == 0 {
		t.Fatal("expected an OA 明天截止 reminder before the change")
	}

	srv := activityServerFull(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)

	// 改期到另一天：旧那天的提醒必须清掉（同时把 key 释放给新的一天）。
	if status := doJSON(t, "PATCH", srv.URL+base+"/assessments/"+itoa(roundID),
		`{"name":"OA","due_at":"2026-12-30T10:00:00Z"}`); status != http.StatusOK {
		t.Fatalf("reschedule assessment -> %d, want 200", status)
	}
	if n := countDueNotices(); n != 0 {
		t.Errorf("after reschedule: %d OA reminders left, want 0 (the stale key pins the old day and mutes the new one)", n)
	}

	// 删掉这一轮：也不该再留着指向它的提醒。
	roundID = insertRound(tomorrow)
	if _, err := reminders.New(db).Run(ctx, time.Now()); err != nil {
		t.Fatalf("reminders: %v", err)
	}
	if countDueNotices() == 0 {
		t.Fatal("expected an OA 明天截止 reminder before delete")
	}
	if status := doJSON(t, "DELETE", srv.URL+base+"/assessments/"+itoa(roundID), ""); status != http.StatusOK {
		t.Fatalf("delete assessment -> %d, want 200", status)
	}
	if n := countDueNotices(); n != 0 {
		t.Errorf("after delete: %d OA reminders left, want 0 (they point at a round that no longer exists)", n)
	}
}
