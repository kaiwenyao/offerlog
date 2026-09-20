// 首页「即将到来的面试 / OA」的可见性口径。
//
// 原始 bug：这两条查询只过滤了 deleted_at，本文件其余每一条统计、周工序条、待办
// 清单以及整个日历都同时过滤 archived_at。于是归档一个岗位之后，日历、待办、
// 计数都干净了，唯独首页最上面那一栏还在广告它的面试。顺带：用户在轮次卡片上
// 点过「标记完成」的面试也不该继续算「即将到来」（提醒生成器用的就是这条规则）。
package integration

import (
	"context"
	"testing"
	"time"

	"offerlog/backend/internal/home"
)

func TestHomeUpcomingHidesArchivedAndCompletedRounds(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	var tz string
	_ = db.Pool().QueryRow(ctx, `SELECT timezone FROM users WHERE id=$1`, owner).Scan(&tz)
	loc, _ := time.LoadLocation(tz)
	soon := time.Now().Add(36 * time.Hour)

	newRounds := func(company string) int64 {
		app := mustCreate(t, svc, owner, company, "Role")
		if _, err := db.Pool().Exec(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
			VALUES($1,$2,'一面','video',$3,$4)`, app.ID, owner, soon, tz); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool().Exec(ctx, `INSERT INTO assessment_rounds(application_id, owner_id, kind, name, progress, planned_at)
			VALUES($1,$2,'online_test','OA','preparing',$3)`, app.ID, owner, soon); err != nil {
			t.Fatal(err)
		}
		return app.ID
	}
	newRounds("LiveCo")
	archived := newRounds("ArchivedCo")
	completedApp := mustCreate(t, svc, owner, "CompletedCo", "Role")
	if _, err := db.Pool().Exec(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone, progress, completed_unknown)
		VALUES($1,$2,'一面','video',$3,$4,'completed',TRUE)`, completedApp.ID, owner, soon, tz); err != nil {
		t.Fatal(err)
	}
	if err := svc.Archive(ctx, owner, archived, true); err != nil {
		t.Fatal(err)
	}

	s, err := home.New(db).Get(ctx, owner, tz, time.Monday, time.Now().In(loc), 20)
	if err != nil {
		t.Fatal(err)
	}

	for _, u := range s.Upcoming {
		if u.CompanyName != "LiveCo" {
			t.Errorf("upcoming interview from %q must not be listed", u.CompanyName)
		}
	}
	if len(s.Upcoming) != 1 {
		t.Errorf("upcoming = %d rows, want only LiveCo's", len(s.Upcoming))
	}
	for _, u := range s.UpcomingAssessments {
		if u.CompanyName != "LiveCo" {
			t.Errorf("upcoming OA from %q must not be listed", u.CompanyName)
		}
	}
	if len(s.UpcomingAssessments) != 1 {
		t.Errorf("upcoming_assessments = %d rows, want only LiveCo's", len(s.UpcomingAssessments))
	}
}
