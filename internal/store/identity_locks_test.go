package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSessionLockAndLeasesAreIndependentOfStateTree(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 12)
	lock, err := s.Lock(ctx, id, id)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	one, err := lock.TreePath("data/one", "history")
	if err != nil {
		t.Fatal(err)
	}
	two, err := lock.TreePath("data/two", "history")
	if err != nil || one == two {
		t.Fatal("backing trees were aliased", one, two, err)
	}
	lease, err := lock.Lease("test")
	if err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := s.Lock(wait, id, id); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("another tree bypassed the session lock", err)
	}
	lock.Close()
	if lock.Held() {
		t.Fatal("released lock remained held")
	}
	current, err := s.Lock(ctx, id, id)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if active, err := current.LiveLeases(); err != nil || len(active) != 1 || active[0].ID != lease.ID {
		t.Fatal("lease depended on backing tree", active, err)
	}
	if _, err := current.Release(lease.ID); err != nil {
		t.Fatal(err)
	}
	for _, tree := range []string{".", "../escape", "data/a/b", "data/../ssh", "/outside"} {
		if _, err := current.TreePath(tree, "."); err == nil {
			t.Fatal("unsafe tree accepted", tree)
		}
	}
}
