// Real-DB regression for the worker lease fence (review round 4, P1): the
// fakeStore in worker/worker_test.go implements "lost lease" by returning
// jobs.ErrLostLease itself — delete the `AND lease_until = $2` guard from
// jobs.go's SQL and that unit test stays green. This test exercises the ACTUAL
// fence: Claim hands back a lease_until; the lease then expires and a second
// worker reclaims the job; the ORIGINAL owner's RenewLease / Succeed / Fail
// must all fail with ErrLostLease and must not have flipped the row.
package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"offerlog/backend/internal/platform/jobs"
)

func TestLeaseFenceLostLeaseFailsStaleOwner(t *testing.T) {
	db, _, _, _ := setup(t)
	ctx := context.Background()
	store := jobs.NewStore(db)

	key := "lease-fence-" + time.Now().Format("150405.000000000")
	if err := store.Enqueue(ctx, "reminders", key, map[string]any{}, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	// Worker A claims the job; its ownership token is the returned lease_until.
	jobA, err := store.Claim(ctx, 60)
	if err != nil {
		t.Fatal(err)
	}
	if jobA == nil || jobA.LeaseUntil == nil {
		t.Fatalf("expected a claimed job with a lease deadline, got %+v", jobA)
	}

	// A crashes without renewing: force the lease to expire (Claim only
	// reclaims status='running' jobs whose lease_until is in the past).
	if _, err := db.Pool().Exec(ctx, `UPDATE jobs SET lease_until = now() - interval '1 second' WHERE id=$1`, jobA.ID); err != nil {
		t.Fatal(err)
	}

	// Worker B reclaims the expired job and gets a NEW lease deadline.
	jobB, err := store.Claim(ctx, 60)
	if err != nil {
		t.Fatal(err)
	}
	if jobB == nil || jobB.ID != jobA.ID {
		t.Fatalf("expected reclaim of the expired job id=%d, got %+v", jobA.ID, jobB)
	}
	if jobB.Attempts <= jobA.Attempts {
		t.Fatalf("reclaim must bump attempts: before=%d after=%d", jobA.Attempts, jobB.Attempts)
	}
	if jobB.LeaseUntil == nil || jobB.LeaseUntil.Equal(*jobA.LeaseUntil) {
		t.Fatal("reclaim must mint a fresh lease deadline")
	}

	// The STALE owner (A), fenced on A's original lease_until, can no longer
	// renew or record any result.
	if err := store.RenewLease(ctx, jobA.ID, 60, jobA.LeaseUntil); !errors.Is(err, jobs.ErrLostLease) {
		t.Fatalf("stale RenewLease err = %v, want ErrLostLease", err)
	}
	if err := store.Succeed(ctx, jobA.ID, jobA.LeaseUntil); !errors.Is(err, jobs.ErrLostLease) {
		t.Fatalf("stale Succeed err = %v, want ErrLostLease", err)
	}
	if err := store.Fail(ctx, jobA.ID, jobA.Attempts, jobA.MaxAttempts, "boom", 0, jobA.LeaseUntil); !errors.Is(err, jobs.ErrLostLease) {
		t.Fatalf("stale Fail err = %v, want ErrLostLease", err)
	}

	// The row must still be owned by B: running with B's lease — A's stale
	// writes must not have marked it done/failed or zeroed the lease.
	var status string
	var lease *time.Time
	if err := db.Pool().QueryRow(ctx, `SELECT status, lease_until FROM jobs WHERE id=$1`, jobA.ID).
		Scan(&status, &lease); err != nil {
		t.Fatal(err)
	}
	if status != "running" || lease == nil {
		t.Fatalf("after stale-owner writes: status=%q lease=%v — must remain running+leased by B", status, lease)
	}

	// B's own fenced completion succeeds.
	if err := store.Succeed(ctx, jobA.ID, jobB.LeaseUntil); err != nil {
		t.Fatalf("current owner Succeed: %v", err)
	}
	var done string
	if err := db.Pool().QueryRow(ctx, `SELECT status FROM jobs WHERE id=$1`, jobA.ID).Scan(&done); err != nil {
		t.Fatal(err)
	}
	if done != "done" {
		t.Fatalf("status after current-owner Succeed = %q, want done", done)
	}
}
