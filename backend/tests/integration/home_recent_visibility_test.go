// 首页「最近动态」的可见性口径。
//
// 原始 bug：这条查询只过滤了 deleted_at，本文件其余每一条统计、周工序条、待办
// 清单、即将到来的面试以及整个日历都同时过滤 archived_at。归档一个岗位之后，
// 它从那些表面全部消失，唯独「最近动态」还把它排在第一条。注释自己写的是
// "active rows"。
package integration

import (
	"context"
	"testing"
	"time"

	"offerlog/backend/internal/home"
)

func TestHomeRecentHidesArchived(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	live := mustCreate(t, svc, owner, "LiveCo", "Role")
	archived := mustCreate(t, svc, owner, "ArchivedCo", "Role")
	if err := svc.Archive(ctx, owner, archived.ID, true); err != nil {
		t.Fatal(err)
	}
	// Touch the archived row after the live one so ORDER BY updated_at DESC
	// would put it first if the query forgot archived_at IS NULL.
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET updated_at=now() WHERE id=$1`, archived.ID); err != nil {
		t.Fatal(err)
	}
	_ = live

	var tz string
	if err := db.Pool().QueryRow(ctx, `SELECT timezone FROM users WHERE id=$1`, owner).Scan(&tz); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation(tz)
	s, err := home.New(db).Get(ctx, owner, tz, time.Monday, time.Now().In(loc), 20)
	if err != nil {
		t.Fatal(err)
	}

	foundLive := false
	for _, r := range s.Recent {
		if r.CompanyName == "ArchivedCo" {
			t.Errorf("archived %q must not appear in recent", r.CompanyName)
		}
		if r.CompanyName == "LiveCo" {
			foundLive = true
		}
	}
	if !foundLive {
		t.Error("live application must still appear in recent")
	}
}
