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
	lock, err := s.Lock(ctx, "dbx-project-111111111111.main", strings.Repeat("a", 32))
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
