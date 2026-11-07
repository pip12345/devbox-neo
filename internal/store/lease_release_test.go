package store

import (
	"context"
	"strings"
	"testing"
)

func TestLeaseReleaseDistinguishesRetiredAttachment(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lock, err := s.Lock(ctx, strings.Repeat("a", 12))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	lease, err := lock.Lease("open")
	if err != nil {
		t.Fatal(err)
	}
	if released, err := lock.Release(lease.ID); err != nil || !released {
		t.Fatal("registered attachment did not own its cleanup", released, err)
	}
	if released, err := lock.Release(lease.ID); err != nil || released {
		t.Fatal("retired attachment was allowed to clean up again", released, err)
	}
}
