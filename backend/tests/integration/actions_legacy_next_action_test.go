// 回归：待办完成后，岗位行上的 next_action 镜像必须一起清掉。
//
// 原始 bug：新建待办会把标题同步到 applications.next_action；完成后这个字段还在，
// 详情发现「没有未完成待办」就把镜像当成迁移遗留再显示一遍，于是出现
// 「待办 (0)，下面却还有一条不可操作的待办」。修复后：完成最后一个未完成待办时
// 镜像清空（next_action_due_at 一并清），还有未完成待办时把镜像切到剩下那条
// （标题 + 截止日期一起改，不能继续指向刚完成的）；撤销完成（reopen）同样要
// 重新同步——镜像取的是「最早的未完成待办」，重开的那条可能正是最早的一条。
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"offerlog/backend/internal/platform/database"
)

// postActionDone calls the today-dashboard endpoint the detail page and home both use.
func postActionDone(t *testing.T, srvURL string, actionID int64, done bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]bool{"done": done})
	res, err := http.Post(srvURL+"/api/v1/actions/"+itoa(actionID)+"/done", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("done action=%d -> %d, want 200", actionID, res.StatusCode)
	}
}

// legacyMirror reads the application's legacy next_action mirror.
func legacyMirror(t *testing.T, db *database.DB, appID int64) (string, *string) {
	t.Helper()
	var action string
	var due *string
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT next_action, to_char(next_action_due_at,'YYYY-MM-DD') FROM applications WHERE id=$1`, appID).
		Scan(&action, &due); err != nil {
		t.Fatal(err)
	}
	return action, due
}

func insertAction(t *testing.T, db *database.DB, appID, owner int64, title string, due *string) int64 {
	t.Helper()
	var id int64
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO actions(application_id, owner_id, title, due_date, done_at, remind_me, priority, source)
		 VALUES($1,$2,$3,$4::date,NULL,FALSE,'medium','manual') RETURNING id`,
		appID, owner, title, due).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCompletingLastActionClearsLegacyNextAction(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "MirrorCo", "Role")
	// 模拟 ActionForm 的镜像写入（新建待办时同步到岗位行）。
	if _, err := db.Pool().Exec(ctx,
		`UPDATE applications SET next_action='跟进 HR', next_action_due_at='2026-09-30' WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	first := insertAction(t, db, app.ID, owner, "跟进 HR", ptr("2026-09-30"))
	second := insertAction(t, db, app.ID, owner, "准备二面", nil)

	srv := newActivityServer(t, db, owner)
	defer srv.Close()

	// 还有一条未完成待办：镜像必须切到剩下那条，不能继续指向刚完成的「跟进 HR」。
	postActionDone(t, srv.URL, first, true)
	if action, due := legacyMirror(t, db, app.ID); action != "准备二面" || due != nil {
		t.Fatalf("完成其中一条后镜像应指向剩下的待办, got action=%q due=%v", action, due)
	}

	// 完成最后一条：镜像一起清空，详情才不会再把它当「旧记录」显示。
	postActionDone(t, srv.URL, second, true)
	action, due := legacyMirror(t, db, app.ID)
	if action != "" {
		t.Fatalf("完成最后一个待办后 next_action 应为空, got %q", action)
	}
	if due != nil {
		t.Fatalf("完成最后一个待办后 next_action_due_at 应为 NULL, got %v", *due)
	}

	// 撤销完成要重新同步：重开的这条又是唯一的未完成待办，表里的下一步必须跟上，
	// 否则列表一直空着而详情里明明有一条待办。
	postActionDone(t, srv.URL, second, false)
	if action, _ := legacyMirror(t, db, app.ID); action != "准备二面" {
		t.Fatalf("reopen 后镜像应指回重开的待办, got %q", action)
	}
}

// 单个待办的普通场景：完成后同样不留旧记录。
func TestCompletingOnlyActionClearsLegacyNextAction(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "OnlyOneCo", "Role")
	if _, err := db.Pool().Exec(ctx,
		`UPDATE applications SET next_action='唯一一件事', next_action_due_ts=now() WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	only := insertAction(t, db, app.ID, owner, "唯一一件事", nil)

	srv := newActivityServer(t, db, owner)
	defer srv.Close()
	postActionDone(t, srv.URL, only, true)

	action, due := legacyMirror(t, db, app.ID)
	if action != "" || due != nil {
		t.Fatalf("全部待办完成后镜像应为空, got action=%q due=%v", action, due)
	}
	var dueTs *string
	if err := db.Pool().QueryRow(ctx, `SELECT next_action_due_ts::text FROM applications WHERE id=$1`, app.ID).Scan(&dueTs); err != nil {
		t.Fatal(err)
	}
	if dueTs != nil {
		t.Fatalf("next_action_due_ts 也应清空, got %v", *dueTs)
	}
}

func ptr[T any](v T) *T { return &v }
