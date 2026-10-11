package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/environment"
)

func TestPreparedCreationStaysPrivateAndDoesNotOverwriteCandidate(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := sessionRecord(t, e, made.SessionID)
	binding := environment.Binding{Workspace: t.TempDir(), LocalName: "prepared"}
	j := transferJournal(t, source, binding, "clone")
	saveTransferJournal(t, e, j)
	spec, err := e.Resolve(ResolveRequest{Workspace: binding.Workspace, LocalName: binding.LocalName, Sources: q.Sources})
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.Store.Lock(ctx, j.DestinationID)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	root, err := l.TreeDir(j.DestinationTree, "harnesses/pi/stores/home/sessions")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "history.jsonl"), "portable history")
	seed := CreationIdentity{ID: j.DestinationID, StateTree: j.DestinationTree, ContainerName: j.ContainerName, Transfer: &j,
		Created: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)}
	started := time.Now().UTC()
	r, _, err := e.CreatePrepared(ctx, l, spec, seed)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != seed.ID || !r.Created.Equal(seed.Created) || r.Activity.Before(started) || r.Action != "create" {
		t.Fatal(r)
	}
	if _, err := l.ReadRecord(ctx); !os.IsNotExist(err) {
		t.Fatal("uncommitted candidate became canonical", err)
	}
	saved, err := e.Store.ReadTransferDestination(ctx, j)
	if err != nil || saved.ID != seed.ID || saved.Applied.Fingerprints != spec.FingerprintsFor(saved.Applied.ImageID) {
		t.Fatal(saved, err)
	}
	if history, err := os.ReadFile(filepath.Join(root, "history.jsonl")); err != nil || string(history) != "portable history" {
		t.Fatal("history lost", err)
	}
	calls := len(d.History())
	if _, _, err := e.CreatePrepared(ctx, l, spec, seed); err == nil {
		t.Fatal("candidate overwritten")
	}
	if len(d.History()) != calls {
		t.Fatal("existing candidate caused runtime mutation")
	}
}

func TestPreparedCreationRequiresValidIdentityAndHeldLock(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	spec, err := e.Resolve(resolveRequest(q))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("f", 12)
	l, err := e.Store.Lock(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.CreatePrepared(ctx, l, spec, CreationIdentity{}); err == nil {
		t.Fatal("accepted missing reservation")
	}
	l.Close()
	if _, _, err := e.CreatePrepared(ctx, l, spec, CreationIdentity{ID: id, Created: time.Now()}); err == nil {
		t.Fatal("accepted unlocked creation")
	}
	if len(d.History()) != 0 {
		t.Fatal("invalid preparation touched runtime")
	}
}
