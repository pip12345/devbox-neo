package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/store"
)

func TestSourceEditingAllowsRepairAndRejectsStaleChains(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	shown := record(t, e, made.Name)
	lock, err := e.Store.Lock(ctx, made.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Touch(shown.ID, "exec"); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	empty, err := e.UpdateSources(ctx, shown, nil)
	if err != nil || empty.ID != shown.ID || empty.Action != "exec" || empty.Applied != shown.Applied {
		t.Fatal("source edit lost unrelated state or rejected an incomplete chain", empty, err)
	}
	var conflict *commanderror.Error
	if _, err := e.UpdateSources(ctx, shown, shown.Sources[:1]); !errors.As(err, &conflict) || conflict.Code != "sources_changed" {
		t.Fatal("stale source editor overwrote another edit", err)
	}
	if err := e.SetDefault(ctx, empty); err != nil {
		t.Fatal("incomplete config blocked default selection", err)
	}
	if _, err := e.Locate(ctx, q.Workspace, ""); err != nil {
		t.Fatal("incomplete config blocked saved lookup", err)
	}
	if _, err := e.Open(ctx, Request{Workspace: made.Name}); err == nil {
		t.Fatal("empty chain became runnable")
	}
	if err := e.Stop(ctx, made.Name, "", false); err != nil {
		t.Fatal("empty chain blocked stop", err)
	}
	if _, err := e.UpdateSources(ctx, empty, shown.Sources); err != nil {
		t.Fatal("could not repair an incomplete chain", err)
	}
}

func TestStaleDefaultDoesNotSelectAReusedLocalName(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	original := record(t, e, made.Name)
	if err := e.SetDefault(ctx, original); err != nil {
		t.Fatal(err)
	}
	key, _ := store.WorkspaceKey(original.Identity.Workspace)
	path := filepath.Join(e.Store.Home, "state/workspaces", key+".json")
	stale, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{made.Name}}, Scope: DeleteSession}); err != nil {
		t.Fatal(err)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("whole-session deletion left a default", selected, err)
	}
	if _, err := e.Create(ctx, q); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, stale, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Locate(ctx, q.Workspace, ""); err == nil {
		t.Fatal("stale default selected the replacement session")
	}
	if err := e.SetDefault(ctx, original); err == nil {
		t.Fatal("stale default picker selected the replacement session")
	}
	if _, err := e.Locate(ctx, made.Name, ""); err != nil {
		t.Fatal("stale default blocked exact targeting", err)
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err := e.List(ctx, q.Workspace)
	if err != nil || len(report.Sessions) != 1 || len(report.DefaultErrors) != 1 {
		t.Fatal("corrupt default hid saved sessions", report, err)
	}
}

func TestConcurrentDefaultSelectionAndSessionDeletionCannotDangle(t *testing.T) {
	for range 4 {
		e, _, q := fixture(t)
		ctx := context.Background()
		made, err := e.Create(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		selected := record(t, e, made.Name)
		var wg sync.WaitGroup
		var deletionErr error
		wg.Add(2)
		go func() { defer wg.Done(); _ = e.SetDefault(ctx, selected) }()
		go func() {
			defer wg.Done()
			_, deletionErr = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{made.Name}}, Scope: DeleteSession})
		}()
		wg.Wait()
		if deletionErr != nil {
			t.Fatal(deletionErr)
		}
		if current, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || current != nil {
			t.Fatal("selection/deletion left a dangling default", current, err)
		}
	}
}

func TestNamedCopyAndMoveDoNotSelectDestinationDefaults(t *testing.T) {
	for _, mode := range []string{"clone", "relocate"} {
		t.Run(mode, func(t *testing.T) {
			e, _, q := fixture(t)
			ctx := context.Background()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			original := record(t, e, made.Name)
			if err := e.SetDefault(ctx, original); err != nil {
				t.Fatal(err)
			}
			result, err := e.Transfer(ctx, TransferOptions{Mode: mode, Source: made.Name, As: "Experiment"})
			if err != nil {
				t.Fatal(err)
			}
			selected, err := e.Store.ReadDefault(ctx, q.Workspace)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "clone" && (selected == nil || selected.Name != made.Name || selected.ID != original.ID) {
				t.Fatal("copy changed the source default", selected)
			}
			if mode == "relocate" && selected != nil {
				t.Fatal("move retained the source default or selected its destination", selected)
			}
			if result.LocalName != "Experiment" || result.Workspace != q.Workspace {
				t.Fatal("lost explicit same-folder destination", result)
			}
			data, _ := json.Marshal(record(t, e, result.Destination).Identity)
			var identity map[string]any
			json.Unmarshal(data, &identity)
			for _, obsolete := range []string{"profile", "project", "slot"} {
				if _, exists := identity[obsolete]; exists {
					t.Fatal("retained obsolete identity metadata", obsolete)
				}
			}
		})
	}
}
