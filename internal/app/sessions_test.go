package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResetPreservesHistoryAuthCacheAndStableBindRoots(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(e.Store.Home, "sessions", result.Name, "harnesses/pi/stores/home")
	history := filepath.Join(base, "sessions/history.json")
	ordinary := filepath.Join(base, "temporary.json")
	auth := filepath.Join(e.Store.Home, "auth/pi/auth.json")
	cache := filepath.Join(e.Store.Home, "cache/harnesses/pi/npm-cache/cache-entry")
	write(t, history, "history")
	write(t, ordinary, "temporary")
	write(t, auth, "{}\n\n")
	write(t, cache, "cache")
	options := ResetOptions{Targets: []string{result.Name}, DryRun: true}
	preview, err := e.ResetSessions(ctx, options)
	if err != nil || len(preview) != 1 {
		t.Fatal(preview, err)
	}
	if string(getFile(t, ordinary)) != "temporary" {
		t.Fatal("dry-run mutated store")
	}
	options.DryRun = false
	if _, err = e.ResetSessions(ctx, options); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(ordinary); !os.IsNotExist(err) {
		t.Fatal("ordinary state survived reset")
	}
	for _, p := range []string{history, auth, cache, base} {
		if _, err = os.Stat(p); err != nil {
			t.Fatal("reset removed protected state/bind root", err)
		}
	}
	if _, err = e.Open(ctx, q); err != nil {
		t.Fatal("reset broke managed synchronization", err)
	}
	options.IncludeHistory = true
	if _, err = e.ResetSessions(ctx, options); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(history); !os.IsNotExist(err) {
		t.Fatal("include-history retained history")
	}
	for _, p := range []string{auth, cache, base} {
		if _, err = os.Stat(p); err != nil {
			t.Fatal("full reset removed auth/cache/root", err)
		}
	}
}
func TestBulkResetDoesNotProceedAgainstRunningContainer(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	a, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Workspace = t.TempDir()
	b, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(e.Store.Home, "sessions", a.Name, "harnesses/pi/stores/home/marker")
	write(t, marker, "keep")
	if _, err = e.Start(ctx, b.Name, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = e.ResetSessions(ctx, ResetOptions{All: true, IncludeHistory: true}); err == nil {
		t.Fatal("running source reset")
	}
	if string(getFile(t, marker)) != "keep" {
		t.Fatal("reset mutated another target before preflight completed")
	}
}
func TestSessionDeleteRejectsContainersAndPreservesExternalLocks(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := record(t, e, result.Name)
	if _, err = e.DeleteSessions(ctx, []string{result.Name}, "", false); err == nil {
		t.Fatal("session deleted under container")
	}
	if _, err = e.DeleteContainers(ctx, Selection{Targets: []string{result.Name}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err = e.DeleteSessions(ctx, []string{result.Name}, "", true); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Store.Read(ctx, result.Name); err != nil {
		t.Fatal("dry-run deleted record", err)
	}
	names, err := e.DeleteSessions(ctx, []string{result.Name}, "", false)
	if err != nil || len(names) != 1 {
		t.Fatal(names, err)
	}
	if _, err = e.Store.Read(ctx, result.Name); !os.IsNotExist(err) {
		t.Fatal("record survived deletion")
	}
	if _, exists := d.Images[r.ImageTag]; exists {
		t.Fatal("session image tag survived deletion")
	}
	lock, err := e.Store.Lock(ctx, result.Name)
	if err != nil {
		t.Fatal("external operation lock was broken", err)
	}
	lock.Close()
}
func TestPruneRequiresConfirmationAndRechecksActivity(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.DeleteContainers(ctx, Selection{All: true}, false); err != nil {
		t.Fatal(err)
	}
	lock, _ := e.Store.Lock(ctx, result.Name)
	r, _ := lock.Load()
	r.Activity = time.Now().Add(-48 * time.Hour)
	if err = lock.Save(r); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	options := PruneOptions{Orphaned: true, OlderThan: 24 * time.Hour, DryRun: true}
	names, err := e.PruneSessions(ctx, options)
	if err != nil || len(names) != 1 {
		t.Fatal(names, err)
	}
	options.DryRun = false
	if _, err = e.PruneSessions(ctx, options); err == nil {
		t.Fatal("unconfirmed prune applied")
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	lock, _ = e.Store.Lock(ctx, result.Name)
	r, _ = lock.Load()
	r.Activity = time.Now()
	lock.Save(r)
	lock.Close()
	if _, err = e.deleteSessionNames(ctx, []string{result.Name}, false, cutoff); err == nil {
		t.Fatal("prune failed to revalidate age")
	}
	options.OlderThan = 0
	options.Confirm = true
	if _, err = e.PruneSessions(ctx, options); err != nil {
		t.Fatal(err)
	}
}
func TestSessionShowDoesNotResolveDesiredConfig(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	details, err := e.SessionShow(ctx, result.Name, "")
	if err != nil || details.Record.ID == "" || !details.Container.Exists {
		t.Fatal(details, err)
	}
}
func TestResetUnlinksSymlinksWithoutFollowingThem(t *testing.T) {
	e, _, q := fixture(t)
	result, err := e.Open(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "data")
	write(t, external, "keep")
	link := filepath.Join(e.Store.Home, "sessions", result.Name, "harnesses/pi/stores/home/link")
	if err = os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	if _, err = e.ResetSessions(context.Background(), ResetOptions{Targets: []string{result.Name}, IncludeHistory: true}); err != nil {
		t.Fatal(err)
	}
	if string(getFile(t, external)) != "keep" {
		t.Fatal("reset followed a state symlink")
	}
}
