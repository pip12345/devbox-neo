package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreparedCreationCommitsIdentityAndActivityWithoutOverwriting(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	spec, err := e.Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.Store.Lock(ctx, "prepared-directory", strings.Repeat("f", 32))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	root, err := l.Dir("harnesses/pi/stores/home/sessions")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "history.jsonl"), "portable history")
	seed := CreationIdentity{ID: strings.Repeat("f", 32), Created: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), Activity: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), Action: "open"}
	r, _, err := e.CreatePrepared(ctx, l, spec, seed)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != seed.ID || !r.Created.Equal(seed.Created) || !r.Activity.Equal(seed.Activity) || r.Action != seed.Action {
		t.Fatalf("identity changed: %+v", r)
	}
	saved, err := l.ReadRecord(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != seed.ID || saved.Applied.Fingerprints != spec.FingerprintsFor(saved.Applied.ImageID) {
		t.Fatal("not a complete ordinary record")
	}
	history, err := os.ReadFile(filepath.Join(root, "history.jsonl"))
	if err != nil || string(history) != "portable history" {
		t.Fatal("history lost")
	}
	calls := len(d.History())
	if _, _, err = e.CreatePrepared(ctx, l, spec, seed); err == nil {
		t.Fatal("existing record overwritten")
	}
	if len(d.History()) != calls {
		t.Fatal("existing record caused Docker mutation")
	}
}
func TestPreparedCreationRequiresValidIdentityAndHeldLock(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	spec, err := e.Resolve(q)
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.Store.Lock(ctx, "prepared-directory", strings.Repeat("f", 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.CreatePrepared(ctx, l, spec, CreationIdentity{}); err == nil {
		t.Fatal("accepted missing identity")
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.CreatePrepared(ctx, l, spec, CreationIdentity{ID: strings.Repeat("a", 32), Created: time.Now()}); err == nil {
		t.Fatal("accepted unlocked creation")
	}
	if len(d.History()) != 0 {
		t.Fatal("invalid identity reached Docker")
	}
}
