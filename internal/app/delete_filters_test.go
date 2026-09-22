package app

import (
	"context"
	"testing"
	"time"

	"devbox/internal/store"
)

func TestDeleteFiltersIntersectAndScopeIsIndependent(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		options              DeleteOptions
		containers, sessions int
	}{
		{"old stopped containers", DeleteOptions{Scope: DeleteContainer, Selection: Selection{Stopped: true}, OlderThan: 24 * time.Hour}, 1, 0},
		{"old orphans", DeleteOptions{Scope: DeleteSession, Orphaned: true, OlderThan: 24 * time.Hour}, 0, 1},
		{"old environments", DeleteOptions{Scope: DeleteSession, OlderThan: 24 * time.Hour}, 2, 3},
		{"all stopped", DeleteOptions{Scope: DeleteContainer, Selection: Selection{All: true, Stopped: true}}, 2, 0},
		{"empty intersection", DeleteOptions{Scope: DeleteSession, Selection: Selection{Stopped: true}, Orphaned: true}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			for _, name := range []string{"old-stopped", "new-stopped", "old-running", "old-missing"} {
				q.Workspace = t.TempDir()
				created, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				if name == "old-running" {
					if _, err = e.Start(ctx, created.Name, ""); err != nil {
						t.Fatal(err)
					}
				}
				if name == "old-missing" {
					d.Forget(created.Name)
				}
				lock, err := e.Store.Lock(ctx, created.Name)
				if err != nil {
					t.Fatal(err)
				}
				r, err := lock.Load()
				if err != nil {
					t.Fatal(err)
				}
				r.Activity = time.Now().Add(-48 * time.Hour)
				if name == "new-stopped" {
					r.Activity = time.Now()
				}
				if err = lock.Save(r); err != nil {
					t.Fatal(err)
				}
				lock.Close()
			}
			preview := tc.options
			preview.DryRun = true
			before := count(d, "rm")
			result, err := e.Delete(ctx, preview)
			if err != nil || len(result.Containers) != tc.containers || len(result.Sessions) != tc.sessions {
				t.Fatal(result, err)
			}
			if count(d, "rm") != before {
				t.Fatal("preview removed containers")
			}
			result, err = e.Delete(ctx, tc.options)
			if err != nil || len(result.Containers) != tc.containers || len(result.Sessions) != tc.sessions {
				t.Fatal(result, err)
			}
		})
	}
}

func TestAgeSelectionIsRecheckedAfterInventoryBeforeMutation(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	setActivity := func(at time.Time) {
		lock, err := e.Store.Lock(ctx, created.Name)
		if err != nil {
			t.Fatal(err)
		}
		r, err := lock.Load()
		if err != nil {
			t.Fatal(err)
		}
		r.Activity = at
		if err = lock.Save(r); err != nil {
			t.Fatal(err)
		}
		lock.Close()
	}
	setActivity(time.Now().Add(-48 * time.Hour))
	touched := false
	d.Fail = func(args []string) error {
		if len(args) > 1 && args[0] == "container" && args[1] == "ls" && !touched {
			touched = true
			setActivity(time.Now())
		}
		return nil
	}
	_, err = e.Delete(ctx, DeleteOptions{Scope: DeleteSession, OlderThan: 24 * time.Hour})
	if err == nil || !touched {
		t.Fatal("stale selection accepted", err)
	}
	if _, exists := d.Snapshot(created.Name); !exists {
		t.Fatal("stale selection deleted container")
	}
}

func TestOrphanSelectionRechecksContainerAbsence(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := e.Store.Lock(ctx, created.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = e.recheckDeleteFilters(ctx, []*store.Locked{lock}, DeleteOptions{Orphaned: true}, time.Time{}); err == nil {
		t.Fatal("orphan recheck accepted existing container")
	}
}

func TestMissingContainerDeletionStillHonorsProfile(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	d.Forget(created.Name)
	_, err = e.Delete(ctx, DeleteOptions{Scope: DeleteSession, Selection: Selection{Targets: []string{created.Name}, LocalName: "other"}})
	if err == nil {
		t.Fatal("profile mismatch ignored for missing container")
	}
	if _, err := e.Store.Read(ctx, created.Name); err != nil {
		t.Fatal("mismatched session was deleted", err)
	}
}
