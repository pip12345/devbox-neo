package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockAllUsesSortedSessionIDs(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"111111111111", "222222222222"}
	locks, err := s.LockAll(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseAll(locks)
	for i, lock := range locks {
		path, err := lock.Path("session.json")
		if err != nil || lock.ID != ids[i] || path != filepath.Join(s.Home, "sessions", ids[i], "session.json") {
			t.Fatal("lock does not use its session ID", lock.ID, path, err)
		}
	}
	CloseAll(locks)
	CloseAll(locks)
	for _, lock := range locks {
		if lock.Held() {
			t.Fatal("closed lock remains held")
		}
	}
}

func TestLockAllRejectsInvalidSetsBeforeAcquiringLocks(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{
		{"222222222222", "111111111111"},
		{"111111111111", "111111111111"},
		{"111111111111", "not-an-id"},
	} {
		if locks, err := s.LockAll(ctx, ids); err == nil {
			CloseAll(locks)
			t.Fatal("invalid lock set accepted", ids)
		}
	}
	entries, err := os.ReadDir(filepath.Join(s.Home, "state/locks/sessions"))
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid set acquired a partial lock", entries, err)
	}
}

func TestLockAllCancellationReleasesPartialSet(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"111111111111", "222222222222"}
	held, err := s.Lock(ctx, ids[1])
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	wait, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if locks, err := s.LockAll(wait, ids); !errors.Is(err, context.DeadlineExceeded) || locks != nil {
		CloseAll(locks)
		t.Fatal("blocked lock set did not cancel", locks, err)
	}
	retry, cancelRetry := context.WithTimeout(ctx, time.Second)
	defer cancelRetry()
	first, err := s.Lock(retry, ids[0])
	if err != nil {
		t.Fatal("cancellation retained an earlier lock", err)
	}
	first.Close()
	if !held.Held() {
		t.Fatal("cancellation released another owner's lock")
	}
}
