package app

import (
	"context"
	"devbox/internal/harness"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransferChecksHarnessPolicyBeforeMutation(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	h, err := harness.Load(e.Store.Home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	h.Definition.Session.Clone = false
	h.Definition.Session.Relocate = false
	b, err := json.Marshal(h.Definition)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "harnesses/pi/harness.json"), string(b))
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	before := count(d, "stop") + count(d, "create")
	for _, mode := range []string{"clone", "relocate"} {
		if _, err = e.Transfer(ctx, TransferOptions{Mode: mode, Source: opened.Name, Destination: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "does not support") {
			t.Fatal(err)
		}
	}
	if count(d, "stop")+count(d, "create") != before {
		t.Fatal("policy preflight changed containers")
	}
}
func TestTransferJournalSurvivesSourceDeletion(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := record(t, e, opened.Name)
	opts := TransferOptions{Mode: "relocate", Source: q.Workspace, Destination: t.TempDir()}
	d.Fail = func(args []string) error {
		if args[0] == "rm" && args[len(args)-1] == source.SetupContainer {
			return errors.New("interruption")
		}
		return nil
	}
	result, err := e.Transfer(ctx, opts)
	if err == nil {
		t.Fatal("ignored interruption")
	}
	d.Fail = nil
	c, exists, err := e.inspect(ctx, source)
	if err != nil || !exists {
		t.Fatal(err)
	}
	if err = e.Docker.Remove(ctx, c, e.owner(source)); err != nil {
		t.Fatal(err)
	}
	lock, err := e.Store.Lock(ctx, opened.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err = lock.Delete(); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	views, err := e.List(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, view := range views {
		if view.Name == opened.Name && view.Pending != nil {
			seen = true
		}
	}
	if !seen {
		t.Fatal("cleanup journal vanished from inventory")
	}
	details, err := e.SessionShow(ctx, opened.Name, "")
	if err != nil || details.Pending == nil {
		t.Fatal("pending cleanup not inspectable", err)
	}
	if _, err = e.Open(ctx, q); err == nil || !strings.Contains(err.Error(), "pending transfer") {
		t.Fatal("source name became available during cleanup", err)
	}
	if _, err = e.Create(ctx, q); err == nil || !strings.Contains(err.Error(), "pending transfer") {
		t.Fatal("create reused a reserved source during cleanup", err)
	}
	// The destination can also lose its container before cleanup finishes. Its
	// committed record remains the recovery authority, not current config.
	d.Forget(result.Destination)
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), "broken")
	if _, err = e.Transfer(ctx, opts); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Store.Read(ctx, opened.Name); !os.IsNotExist(err) {
		t.Fatal("source returned", err)
	}
	if _, exists := d.Snapshot(result.Destination); !exists {
		t.Fatal("destination not recovered")
	}
}
func TestTransferRetriesPreparedButUncommittedDestination(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := record(t, e, opened.Name)
	rel := "harnesses/pi/stores/home/sessions/history.json"
	write(t, filepath.Join(e.Store.Home, "sessions", opened.Name, rel), "first snapshot")
	opts := TransferOptions{Mode: "clone", Source: opened.Name, Destination: t.TempDir()}
	d.Fail = func(args []string) error {
		if args[0] == "stop" && args[len(args)-1] != source.SetupContainer {
			return errors.New("destination stop unavailable")
		}
		return nil
	}
	result, err := e.Transfer(ctx, opts)
	if err == nil {
		t.Fatal("ignored failure")
	}
	prepared := record(t, e, result.Destination)
	journal, err := e.Store.ReadTransfer(opened.Name)
	if err != nil || journal == nil || journal.Phase != "prepare" {
		t.Fatal(journal, err)
	}
	write(t, filepath.Join(e.Store.Home, "sessions", opened.Name, rel), "new authoritative snapshot")
	d.Fail = nil
	if _, err = e.Transfer(ctx, opts); err != nil {
		t.Fatal(err)
	}
	current := record(t, e, result.Destination)
	if current.ID != prepared.ID || current.SetupContainer == prepared.SetupContainer {
		t.Fatal("retry did not retain identity and replace preparation")
	}
	data, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", result.Destination, rel))
	if err != nil || string(data) != "new authoritative snapshot" {
		t.Fatal("stale snapshot won", err)
	}
}
func TestTransferCancellationRestoresSource(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(ctx, opened.Name, ""); err != nil {
		t.Fatal(err)
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	d.Fail = func(args []string) error {
		if args[0] == "build" {
			cancel()
			return context.Canceled
		}
		return nil
	}
	_, err = e.Transfer(run, TransferOptions{Mode: "relocate", Source: opened.Name, Destination: t.TempDir()})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	c, _ := d.Snapshot(opened.Name)
	if !c.State.Running {
		t.Fatal("cancelled operation did not restart source")
	}
}
func TestTransferCopiesOpaqueLinksNotAuthOrCaches(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	opened, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(e.Store.Home, "sessions", opened.Name, "harnesses/pi/stores/home")
	write(t, filepath.Join(base, "sessions/keep.json"), "keep")
	write(t, filepath.Join(base, "auth.json"), "excluded auth overlay placeholder")
	external := filepath.Join(t.TempDir(), "outside")
	write(t, external, "not traversed")
	if err = os.Symlink(external, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(e.Store.Home, "cache/harnesses/pi/transfer-marker")
	write(t, cache, "shared")
	result, err := e.Transfer(ctx, TransferOptions{Mode: "clone", Source: opened.Name, Destination: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(e.Store.Home, "sessions", result.Destination, "harnesses/pi/stores/home")
	link, err := os.Readlink(filepath.Join(target, "link"))
	if err != nil || link != external {
		t.Fatal("opaque link not preserved", err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "auth.json")); err == nil && string(b) == "excluded auth overlay placeholder" {
		t.Fatal("auth overlay copied")
	}
	if b, err := os.ReadFile(cache); err != nil || string(b) != "shared" {
		t.Fatal("shared cache changed", err)
	}
}
