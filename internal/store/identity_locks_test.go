package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSharedIdentitySerializesDifferentDirectoriesAndSharesLeases(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	locks, err := s.LockAll(ctx, []string{"first", "second"}, map[string]string{"first": id, "second": id})
	if err != nil {
		t.Fatal(err)
	}
	defer CloseAll(locks)
	lease, err := locks[0].Lease("test")
	if err != nil {
		t.Fatal(err)
	}
	active, err := locks[1].LiveLeases()
	if err != nil || len(active) != 1 || active[0].ID != lease.ID {
		t.Fatal("lease depended on storage directory", active, err)
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := s.Lock(wait, "third", id); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("directory change bypassed the ID lock", err)
	}
	CloseAll(locks)
	if locks[0].Held() || locks[1].Held() {
		t.Fatal("shared lock remained held")
	}
	l, err := s.Lock(ctx, "third", id)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.Release(lease.ID); err != nil {
		t.Fatal(err)
	}
}
