// 无岗位待办（application_id IS NULL）的删除回归。
//
// DeleteActionAndSettle 原本这样读它属于哪个岗位：
//
//	var appID int64
//	...Scan(&appID)
//	if appID == 0 { /* 跨岗位待办没有镜像可清 */ }
//
// actions.application_id 是可空列（00001_init.sql），pgx 把 NULL 扫进 int64
// 会直接报 `cannot scan NULL into *int64`——于是那个 appID == 0 分支永远不可达
// （死分支），DELETE 反而返 500 且行还在。当前没有创建无岗位待办的入口，所以这
// 是一颗哑弹；但只要哪天开了「跨岗位待办」，删除路径第一个炸。
//
// 这个测试直接插一行 application_id IS NULL 的 action 来钉住正确行为：删除成功、
// 行消失、重复删除仍然幂等；同时确认普通岗位待办的老行为（清 next_action 镜像）
// 没有被这次改动带坏。
package integration

import (
	"context"
	"net/http"
	"testing"

	actrepo "offerlog/backend/internal/activities/repository"
	"offerlog/backend/internal/platform/database"
)

// insertCrossAppAction inserts an action that belongs to no application — the
// shape 跨岗位待办 will take (actions.application_id is nullable).
func insertCrossAppAction(t *testing.T, db *database.DB, owner int64, title string) int64 {
	t.Helper()
	var id int64
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO actions(application_id, owner_id, title, done_at, remind_me, priority, source)
		 VALUES(NULL,$1,$2,NULL,FALSE,'medium','manual') RETURNING id`,
		owner, title).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// deleteActionHTTP calls the standalone /actions root exactly like the
// frontend does (tabs.tsx) and returns the status code.
func deleteActionHTTP(t *testing.T, srvURL string, actionID int64) int {
	t.Helper()
	req, _ := http.NewRequest("DELETE", srvURL+"/api/v1/actions/"+itoa(actionID), nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode
}

func actionExists(t *testing.T, db *database.DB, owner, id int64) bool {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM actions WHERE id=$1 AND owner_id=$2`, id, owner).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func TestDeleteCrossApplicationAction(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()

	// 一条普通岗位待办（用来确认镜像收尾没被带坏）+ 两条无岗位待办。
	app := mustCreate(t, svc, owner, "CrossAppCo", "Role")
	appAction := insertAction(t, db, app.ID, owner, "岗位内待办", nil)
	crossA := insertCrossAppAction(t, db, owner, "跨岗位待办 A")
	crossB := insertCrossAppAction(t, db, owner, "跨岗位待办 B")

	srv := newActivityServer(t, db, owner)
	defer srv.Close()

	// 无岗位待办：必须 200，且行真的消失（旧代码在这里 500 且行还在）。
	res := deleteActionHTTP(t, srv.URL, crossA)
	if res != http.StatusOK {
		t.Fatalf("delete cross-app action -> %d, want 200 "+
			"(NULL application_id must be scanned as nullable, not into int64)", res)
	}
	if actionExists(t, db, owner, crossA) {
		t.Fatalf("cross-app action %d still exists after delete", crossA)
	}
	// 幂等：再删一次仍然是 200（行已不在 = 当成删掉了）。
	if res := deleteActionHTTP(t, srv.URL, crossA); res != http.StatusOK {
		t.Fatalf("re-delete cross-app action -> %d, want 200", res)
	}

	// 删一条无岗位待办不能碰另一条。
	if !actionExists(t, db, owner, crossB) {
		t.Fatalf("deleting cross-app action A also removed unrelated action B")
	}

	// 普通岗位待办：删除仍然成功，且 next_action 镜像照老规矩收尾。
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET next_action='旧镜像', version=version+1
		WHERE id=$1 AND owner_id=$2`, app.ID, owner); err != nil {
		t.Fatal(err)
	}
	if res := deleteActionHTTP(t, srv.URL, appAction); res != http.StatusOK {
		t.Fatalf("delete app-scoped action -> %d, want 200", res)
	}
	var mirror string
	if err := db.Pool().QueryRow(ctx, `SELECT next_action FROM applications WHERE id=$1`, app.ID).
		Scan(&mirror); err != nil {
		t.Fatal(err)
	}
	if mirror != "" {
		t.Fatalf("app-scoped delete must still retire the legacy mirror, got %q", mirror)
	}
}

// TestDeleteActionUnknownIDIsIdempotent pins the "row not found = already
// deleted" contract shared by every delete path.
func TestDeleteActionUnknownIDIsIdempotent(t *testing.T) {
	db, _, _, owner := setup(t)
	srv := newActivityServer(t, db, owner)
	defer srv.Close()

	if res := deleteActionHTTP(t, srv.URL, 999999999); res != http.StatusOK {
		t.Fatalf("delete unknown action -> %d, want 200", res)
	}
	// repo 层同样不报错（DeleteActionAndSettle 的幂等分支）。
	if err := actrepo.New(db).DeleteActionAndSettle(context.Background(), owner, 999999999); err != nil {
		t.Fatalf("DeleteActionAndSettle on unknown id = %v, want nil", err)
	}
}
