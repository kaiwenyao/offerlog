// 归档 / 回收站里的岗位不该继续提醒「明天有面试」。
//
// 原始 bug：逾期待办、OA 截止、未回复跟进三类提醒都带着
// `ap.deleted_at IS NULL AND ap.archived_at IS NULL`，唯独面试提醒漏了。更糟的是
// 归档与软删除会调用 clearAppReminders —— 那是一条 DELETE，会把已生成的通知连同
// 幂等键一起抹掉（好让 restore / unarchive 之后能重新提醒）。于是这条漏掉守卫的
// 查询在第二天的扫描里又把「明天有面试」原样造了回来：用户忽略多少次都会复活。
package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"offerlog/backend/internal/prefs"
	"offerlog/backend/internal/reminders"
)

func TestInterviewReminderSkipsArchivedAndTrashedApps(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	_ = prefs.New(db).Upsert(ctx, &prefs.Preferences{
		UserID: owner, Timezone: "Europe/Dublin", WeekStart: 1,
		RemindOverdue: false, RemindInterview: true, RemindStaleDays: 0,
	})

	tomorrow := time.Now().Add(24 * time.Hour)
	newInterview := func(company string) (appID, interviewID int64) {
		app := mustCreate(t, svc, owner, company, "Role")
		var iid int64
		if err := db.Pool().QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
			VALUES($1,$2,'一面','video',$3,'Europe/Dublin') RETURNING id`, app.ID, owner, tomorrow).Scan(&iid); err != nil {
			t.Fatal(err)
		}
		return app.ID, iid
	}
	liveApp, liveIV := newInterview("LiveCo")
	archivedApp, archivedIV := newInterview("ArchivedCo")
	trashedApp, trashedIV := newInterview("TrashedCo")

	if err := svc.Archive(ctx, owner, archivedApp, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SoftDelete(ctx, owner, trashedApp); err != nil {
		t.Fatal(err)
	}

	if _, err := reminders.New(db).Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}

	count := func(iid int64) int {
		var n int
		_ = db.Pool().QueryRow(ctx, `SELECT count(*) FROM notifications
			WHERE owner_id=$1 AND kind='interview' AND idempotency_key LIKE $2`,
			owner, fmt.Sprintf("interview:%d:%%", iid)).Scan(&n)
		return n
	}

	if got := count(liveIV); got != 1 {
		t.Errorf("live application: %d interview reminders, want 1", got)
	}
	if got := count(archivedIV); got != 0 {
		t.Errorf("archived application: %d interview reminders, want 0", got)
	}
	if got := count(trashedIV); got != 0 {
		t.Errorf("trashed application: %d interview reminders, want 0", got)
	}
	_ = liveApp

	// 取消归档之后必须重新提醒：守卫是「现在藏起来了就别响」，不是永久静音。
	if err := svc.Archive(ctx, owner, archivedApp, false); err != nil {
		t.Fatal(err)
	}
	if _, err := reminders.New(db).Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := count(archivedIV); got != 1 {
		t.Errorf("after unarchive: %d interview reminders, want 1", got)
	}
}
