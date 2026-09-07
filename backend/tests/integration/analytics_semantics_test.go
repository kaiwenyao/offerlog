// Analytics semantic integration coverage (plan §4.2): empty sample shows
// null rates with a zero denominator (frontend renders — 暂无样本);
// all-unreplied shows 0% with a positive denominator; partial reply shows the
// correct ratio; the cohort is computed over the full dataset — a 250-row
// dataset yields exact counts, never a page-derived total.
package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"offerlog/backend/internal/analytics"
	appservice "offerlog/backend/internal/applications/service"
)

func TestAnalyticsEmptySampleNullRates(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	repo := analytics.New(db)
	// An owner with only saved (unsubmitted) applications: submitted cohort empty.
	for i := 0; i < 3; i++ {
		if _, err := svc.Create(ctx, owner, &appservice.CreateInput{
			CompanyName: fmt.Sprintf("Empty%d", i), Position: "Role",
		}); err != nil {
			t.Fatal(err)
		}
	}
	m, err := repo.Counts(ctx, &analytics.SnapshotRequest{OwnerID: owner, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if m.Denominator != 0 {
		t.Fatalf("denominator = %d, want 0 (no submitted apps)", m.Denominator)
	}
	if m.ResponseRate != nil || m.InterviewRate != nil || m.OfferRate != nil {
		t.Fatalf("rates must be null when no sample: %+v", m)
	}
	if m.Responded != 0 || m.PendingResponse != 0 {
		t.Fatalf("empty numerators must be 0: %+v", m)
	}
}

func TestAnalyticsZeroVsPartialReply(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	repo := analytics.New(db)
	now := time.Now()
	// 10 submitted; 4 replied → 40%, 6 pending.
	sub := now.Add(-48 * time.Hour)
	for i := 0; i < 10; i++ {
		row, err := svc.Create(ctx, owner, &appservice.CreateInput{
			CompanyName: fmt.Sprintf("Reply%d", i), Position: "Role", Status: "applied", SubmittedAt: &sub,
		})
		if err != nil {
			t.Fatal(err)
		}
		if i < 4 {
			resp := now.Add(-24 * time.Hour)
			if _, err := svc.Transition(ctx, owner, row.ID, &appservice.TransitionInput{
				ToStatus: "screening", Version: 1, FirstResponseAt: &resp,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	m, err := repo.Counts(ctx, &analytics.SnapshotRequest{OwnerID: owner, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if m.Denominator != 10 {
		t.Fatalf("denominator=%d want 10", m.Denominator)
	}
	if m.ResponseRate == nil || *m.ResponseRate != 0.4 {
		t.Fatalf("response rate = %v want 0.4", m.ResponseRate)
	}
	if m.Responded != 4 || m.PendingResponse != 6 {
		t.Fatalf("responded=%d pending=%d want 4/6", m.Responded, m.PendingResponse)
	}
	if m.OfferRate == nil || *m.OfferRate != 0 {
		t.Fatalf("offer rate over non-empty cohort must be 0 (not null): %v", m.OfferRate)
	}
}

func TestAnalyticsCohortOverFullDataset(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	repo := analytics.New(db)
	sub := time.Now().Add(-72 * time.Hour)
	for i := 0; i < 250; i++ {
		if _, err := svc.Create(ctx, owner, &appservice.CreateInput{
			CompanyName: fmt.Sprintf("Big%d", i), Position: "Role", Status: "applied", SubmittedAt: &sub,
		}); err != nil {
			t.Fatal(err)
		}
	}
	m, err := repo.Counts(ctx, &analytics.SnapshotRequest{OwnerID: owner, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if m.Denominator != 250 {
		t.Fatalf("cohort = %d, want 250 (full dataset, not a page)", m.Denominator)
	}
	if m.SubmittedCount != 250 {
		t.Fatalf("submitted_count = %d, want 250", m.SubmittedCount)
	}
}
