package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionDeletionPreservesExternalLocksAuthCacheAndWorkspace(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := record(t, e, result.Name)
	paths := []string{
		filepath.Join(e.Store.Home, "auth/pi/auth.json"),
		filepath.Join(e.Store.Home, "cache/harnesses/pi/npm-cache/entry"),
		filepath.Join(q.Workspace, "source.txt"),
	}
	for _, p := range paths {
		write(t, p, "keep")
	}
	options := DeleteOptions{Selection: Selection{Targets: []string{result.Name}}, Scope: DeleteSession, DryRun: true}
	if _, err := e.Delete(ctx, options); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.Read(ctx, result.Name); err != nil {
		t.Fatal("dry run deleted record", err)
	}
	options.DryRun = false
	removed, err := e.Delete(ctx, options)
	if err != nil || len(removed.Sessions) != 1 {
		t.Fatal(removed, err)
	}
	if _, err := e.Store.Read(ctx, result.Name); !os.IsNotExist(err) {
		t.Fatal("record survived deletion", err)
	}
	if _, exists := d.Images[r.ImageTag]; exists {
		t.Fatal("session image tag survived deletion")
	}
	for _, p := range paths {
		if string(getFile(t, p)) != "keep" {
			t.Fatal("unrelated data removed", p)
		}
	}
	lock, err := e.Store.Lock(ctx, result.Name)
	if err != nil {
		t.Fatal("external operation lock was broken", err)
	}
	lock.Close()
}

func TestSessionDeletionDoesNotFollowStoreSymlinks(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "data")
	write(t, external, "keep")
	link := filepath.Join(e.Store.Home, "sessions", result.Name, "harnesses/pi/stores/home/link")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{result.Name}}, Scope: DeleteSession}); err != nil {
		t.Fatal(err)
	}
	if string(getFile(t, external)) != "keep" {
		t.Fatal("deletion followed a state symlink")
	}
}
