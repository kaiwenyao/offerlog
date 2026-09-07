package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"offerlog/backend/internal/platform/jobs"
)

// fakeStore is a minimal in-memory jobs.Store double for unit-testing the
// registry's success/failure routing without a database.
type fakeStore struct {
	mu      sync.Mutex
	status  string
	lastErr string
	failN   int
	okN     int
	renewN  int
	// lostLease makes RenewLease/Succeed/Fail report ErrLostLease (simulating
	// a lease that expired and was reclaimed by another worker).
	lostLease bool
}

func (f *fakeStore) RenewLease(ctx context.Context, id int64, leaseSeconds int, claimed *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lostLease {
		return jobs.ErrLostLease
	}
	f.renewN++
	return nil
}

func (f *fakeStore) Succeed(ctx context.Context, id int64, claimed *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lostLease {
		return jobs.ErrLostLease
	}
	f.okN++
	f.status = "done"
	return nil
}

func (f *fakeStore) Fail(ctx context.Context, id int64, attempts, maxAttempts int, msg string, backoff time.Duration, claimed *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lostLease {
		return jobs.ErrLostLease
	}
	f.failN++
	f.status = "failed"
	f.lastErr = msg
	return nil
}

func TestUnknownKindFails(t *testing.T) {
	reg := NewRegistry()
	store := &fakeStore{}
	job := &jobs.Job{ID: 1, Kind: "totally_unknown", Attempts: 1, MaxAttempts: 5}
	until := time.Now().Add(time.Minute)
	job.LeaseUntil = &until
	if err := reg.Process(context.Background(), store, job); err != nil {
		t.Fatalf("unknown kind should be recorded as terminal failure, not an error: %v", err)
	}
	if store.status != "failed" {
		t.Fatalf("unknown kind must not succeed; status = %q", store.status)
	}
	if store.lastErr == "" {
		t.Fatal("unknown kind must carry a failure reason")
	}
}

func TestHandlerErrorRecordsFailureAndRetry(t *testing.T) {
	reg := NewRegistry()
	calls := 0
	reg.Register("flaky", func(ctx context.Context, store Store, job *jobs.Job) error {
		calls++
		return errors.New("boom")
	})
	store := &fakeStore{}
	job := &jobs.Job{ID: 2, Kind: "flaky", Attempts: 2, MaxAttempts: 5}
	until := time.Now().Add(time.Minute)
	job.LeaseUntil = &until
	if err := reg.Process(context.Background(), store, job); err != nil {
		t.Fatalf("handler failure is recorded, not propagated: %v", err)
	}
	if store.status != "failed" || calls != 1 || store.failN != 1 {
		t.Fatalf("want recorded failure, got status=%q calls=%d fails=%d", store.status, calls, store.failN)
	}
	if store.lastErr != "boom" {
		t.Fatalf("failure reason = %q, want boom", store.lastErr)
	}
}

func TestHandlerSuccess(t *testing.T) {
	reg := NewRegistry()
	reg.Register("ok", func(ctx context.Context, store Store, job *jobs.Job) error { return nil })
	store := &fakeStore{}
	job := &jobs.Job{ID: 3, Kind: "ok", Attempts: 1, MaxAttempts: 5}
	until := time.Now().Add(time.Minute)
	job.LeaseUntil = &until
	if err := reg.Process(context.Background(), store, job); err != nil {
		t.Fatal(err)
	}
	if store.status != "done" || store.okN != 1 {
		t.Fatalf("want success, got %+v", store)
	}
}

// TestHandlerOutlivingLeaseRenews verifies the renewal ticker fires while a
// handler runs longer than the renew interval.
func TestHandlerOutlivingLeaseRenews(t *testing.T) {
	reg := NewRegistry()
	// short lease/renew so the test doesn't take 20s
	reg.WithLease(2, 100*time.Millisecond)
	reg.Register("slow", func(ctx context.Context, store Store, job *jobs.Job) error {
		time.Sleep(350 * time.Millisecond)
		return nil
	})
	store := &fakeStore{}
	job := &jobs.Job{ID: 4, Kind: "slow", Attempts: 1, MaxAttempts: 5}
	until := time.Now().Add(2 * time.Second)
	job.LeaseUntil = &until
	if err := reg.Process(context.Background(), store, job); err != nil {
		t.Fatal(err)
	}
	if store.okN != 1 || store.status != "done" {
		t.Fatalf("want success after renewal, got %+v", store)
	}
	if store.renewN == 0 {
		t.Fatal("expected at least one lease renewal while the handler ran")
	}
}

// TestLostLeaseDoesNotWriteResult verifies fencing: once the lease is lost
// (reclaimed by another worker), RenewLease/Succeed/Fail stop and the stale
// worker cannot mark the job done.
func TestLostLeaseDoesNotWriteResult(t *testing.T) {
	reg := NewRegistry()
	reg.Register("stale", func(ctx context.Context, store Store, job *jobs.Job) error { return nil })
	store := &fakeStore{lostLease: true}
	job := &jobs.Job{ID: 5, Kind: "stale", Attempts: 1, MaxAttempts: 5}
	until := time.Now().Add(1 * time.Second)
	job.LeaseUntil = &until
	// The renewal goroutine returns after logging; the handler completes and
	// Succeed reports the lost lease. Process surfaces that as the error.
	err := reg.Process(context.Background(), store, job)
	if err == nil {
		t.Fatal("expected ErrLostLease surfaced when the job is no longer owned")
	}
	if store.okN != 0 {
		t.Fatalf("stale worker must not mark done: %+v", store)
	}
}
