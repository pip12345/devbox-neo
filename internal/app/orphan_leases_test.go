package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"devbox/internal/commanderror"
)

func TestMissingRecordCannotBypassSessionIDLease(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	lock, err := e.Store.Lock(ctx, r.Directory, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := lock.Lease("test")
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	path, err := e.Store.RecordPath(r.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{r.ID, r.Applied.Creation.Name} {
		_, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{target}}, Scope: DeleteContainer})
		var busy *commanderror.Error
		if !errors.As(err, &busy) || busy.Code != "session_busy" {
			t.Fatal("missing record bypassed active command protection", target, err)
		}
	}
	if _, exists := d.Snapshot(r.Applied.Creation.Name); !exists {
		t.Fatal("removed a busy container")
	}
	lock, err = e.Store.Lock(ctx, r.Directory, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Release(lease.ID); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{r.ID}}, Scope: DeleteContainer}); err != nil {
		t.Fatal(err)
	}
	if _, exists := d.Snapshot(r.Applied.Creation.Name); exists {
		t.Fatal("idle orphan not removed")
	}
}
