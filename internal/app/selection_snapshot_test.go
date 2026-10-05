package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/environment"
)

func TestCapturedDeletionKeepsSessionAfterDefaultChanges(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	chosen := sessionRecord(t, e, first.SessionID)
	if err := e.SetDefault(ctx, chosen); err != nil {
		t.Fatal(err)
	}
	q.LocalName = "second"
	second, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{q.Workspace}}, Scope: DeleteContainer, DryRun: true})
	if err != nil || len(preview.Targets) != 1 || preview.Targets[0].Name() != chosen.Directory {
		t.Fatal(preview, err)
	}
	if err := e.SetDefault(ctx, sessionRecord(t, e, second.SessionID)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Captured: preview.Targets}, Scope: DeleteContainer}); err != nil {
		t.Fatal(err)
	}
	if _, exists := d.Snapshot(chosen.Applied.Creation.Name); exists {
		t.Fatal("captured session was not removed")
	}
	if _, exists := sessionSnapshot(t, e, second.SessionID); !exists {
		t.Fatal("changed default retargeted deletion")
	}
	if current, err := e.Locate(ctx, q.Workspace, ""); err != nil || current.ID != second.SessionID {
		t.Fatal("container deletion changed default selection", current, err)
	}
}

func TestSelectionReturnsFactsWithoutMutatingRequest(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	request := Selection{Targets: []string{r.Directory, r.Directory}}
	for range 2 {
		targets, err := e.selectContainers(ctx, request)
		if err != nil || len(targets) != 1 || targets[0].sessionID != r.ID || targets[0].record != recordPresent {
			t.Fatal(targets, err)
		}
		if len(request.Targets) != 2 || len(request.Captured) != 0 {
			t.Fatal("discovery mutated its request", request)
		}
	}
}

func TestCapturedOrphanRejectsReplacementRuntime(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	path, _ := e.Store.RecordPath(r.Directory)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	preview, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{r.Applied.Creation.Name}}, Scope: DeleteContainer, DryRun: true})
	if err != nil || len(preview.Targets) != 1 {
		t.Fatal(preview, err)
	}
	if preview.Targets[0].sessionID != r.ID || preview.Targets[0].record != recordAbsent {
		t.Fatal("orphan lost its lock owner", preview.Targets)
	}
	c, _ := d.Snapshot(r.Applied.Creation.Name)
	c.ID = strings.Repeat("b", 64)
	d.Containers[r.Applied.Creation.Name] = c
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Captured: preview.Targets}, Scope: DeleteContainer}); err == nil {
		t.Fatal("captured orphan adopted a replacement runtime")
	}
	if _, exists := d.Snapshot(r.Applied.Creation.Name); !exists {
		t.Fatal("replacement runtime was deleted")
	}
}

func TestCapturedIncompleteDeletionRetainsDirectoryIdentity(t *testing.T) {
	e, _, _ := fixture(t)
	ctx := context.Background()
	name := environment.ResourceName(t.TempDir(), "incomplete", "allocation")
	root := filepath.Join(e.Store.Home, "sessions", name)
	write(t, filepath.Join(root, "marker"), "original")
	preview, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{name}}, Scope: DeleteSession, DryRun: true})
	if err != nil || len(preview.Targets) != 1 {
		t.Fatal(preview, err)
	}
	if err := os.Rename(root, root+"-retained"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "marker"), "replacement")
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Captured: preview.Targets}, Scope: DeleteSession}); err == nil {
		t.Fatal("captured incomplete directory adopted replacement files")
	}
	if got := getFile(t, filepath.Join(root, "marker")); string(got) != "replacement" {
		t.Fatal("replacement files changed")
	}
}
