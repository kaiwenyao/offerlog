package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"offerlog/backend/internal/platform/jobs"
)

// fakeStore is a minimal in-memory jobs.Store double for unit-testing the
// registry's success/failure routing without a database.
type fakeStore struct {
	status  string
	lastErr string
	failN   int
	okN     int
}

func (f *fakeStore) Succeed(ctx context.Context, id int64) error {
	f.okN++
	f.status = "done"
	return nil
}

func (f *fakeStore) Fail(ctx context.Context, id int64, attempts, maxAttempts int, msg string, backoff time.Duration) error {
	f.failN++
	f.status = "failed"
	f.lastErr = msg
	return nil
}

func TestUnknownKindFails(t *testing.T) {
	reg := NewRegistry()
	store := &fakeStore{}
	job := &jobs.Job{ID: 1, Kind: "totally_unknown", Attempts: 1, MaxAttempts: 5}
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
	if err := reg.Process(context.Background(), store, job); err != nil {
		t.Fatal(err)
	}
	if store.status != "done" || store.okN != 1 {
		t.Fatalf("want success, got %+v", store)
	}
}
