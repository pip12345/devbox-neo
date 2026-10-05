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
	"devbox/internal/config"
)

func TestSourceEditingAllowsRepairAndRejectsStaleChains(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	shown := sessionRecord(t, e, made.SessionID)
	lock, err := e.Store.Lock(ctx, sessionRecord(t, e, made.SessionID).Directory, sessionRecord(t, e, made.SessionID).ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Touch(shown.ID, "exec"); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	var required *commanderror.Error
	if _, err := e.UpdateSources(ctx, shown, nil); !errors.As(err, &required) || required.Code != "configs_required" {
		t.Fatal("accepted an empty saved selection", err)
	}
	missing := []config.Reference{{Label: "missing", Kind: config.ReferenceFixed, Path: filepath.Join(e.Store.Home, "configs", "missing")}}
	incomplete, err := e.UpdateSources(ctx, shown, missing)
	if err != nil || incomplete.ID != shown.ID || incomplete.Action != "exec" || incomplete.Applied.Fingerprints != shown.Applied.Fingerprints {
		t.Fatal("source edit lost unrelated state or rejected an incomplete chain", incomplete, err)
	}
	var conflict *commanderror.Error
	if _, err := e.UpdateSources(ctx, shown, shown.Settings.Sources[:1]); !errors.As(err, &conflict) || conflict.Code != "sources_changed" {
		t.Fatal("stale source editor overwrote another edit", err)
	}
	if err := e.SetDefault(ctx, incomplete); err != nil {
		t.Fatal("incomplete config blocked default selection", err)
	}
	if _, err := e.Locate(ctx, q.Workspace, ""); err != nil {
		t.Fatal("incomplete config blocked saved lookup", err)
	}
	if _, err := e.Open(ctx, OpenRequest{Target: made.SessionID}); err != nil {
		t.Fatal("missing desired config blocked applied access", err)
	}
	if _, err := e.Recreate(ctx, RecreateRequest{Target: made.SessionID}, false); err == nil {
		t.Fatal("missing config became applicable")
	}
	if err := e.Stop(ctx, made.SessionID, "", false); err != nil {
		t.Fatal("missing config blocked stop", err)
	}
	// Existing empty records remain readable and repairable. The nonempty rule
	// belongs to saved edits, not record decoding or creation drafts.
	lock, err = e.Store.Lock(ctx, sessionRecord(t, e, made.SessionID).Directory, sessionRecord(t, e, made.SessionID).ID)
	if err != nil {
		t.Fatal(err)
	}
	incomplete.Settings.Sources = nil
	err = lock.Save(incomplete)
	lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	empty := sessionRecord(t, e, made.SessionID)
	if _, err := e.UpdateSources(ctx, empty, shown.Settings.Sources); err != nil {
		t.Fatal("could not repair an empty chain", err)
	}
}

func TestStaleDefaultDoesNotSelectAReusedLocalName(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	original := sessionRecord(t, e, made.SessionID)
	if err := e.SetDefault(ctx, original); err != nil {
		t.Fatal(err)
	}
	for _, localName := range []string{"", q.LocalName} {
		name, id, err := e.transferSource(ctx, TransferOptions{Source: q.Workspace, LocalName: localName})
		if err != nil || name != original.Directory || id != original.ID {
			t.Fatal("transfer source selection lost its durable-ID snapshot", name, id, err)
		}
	}
	path := filepath.Join(e.Store.Home, "state/folder-defaults.json")
	stale, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{made.SessionID}}, Scope: DeleteSession}); err != nil {
		t.Fatal(err)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("whole-session deletion left a default", selected, err)
	}
	replacement, err := e.Create(ctx, q)
	if err != nil {
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
	if _, err := e.Locate(ctx, replacement.SessionID, ""); err != nil {
		t.Fatal("stale default blocked exact targeting", err)
	}
	if _, err := e.Locate(ctx, made.SessionID, ""); err == nil {
		t.Fatal("deleted ID selected the replacement")
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
		selected := sessionRecord(t, e, made.SessionID)
		var wg sync.WaitGroup
		var deletionErr error
		wg.Add(2)
		go func() { defer wg.Done(); _ = e.SetDefault(ctx, selected) }()
		go func() {
			defer wg.Done()
			_, deletionErr = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{made.SessionID}}, Scope: DeleteSession})
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
			original := sessionRecord(t, e, made.SessionID)
			if err := e.SetDefault(ctx, original); err != nil {
				t.Fatal(err)
			}
			result, err := e.Transfer(ctx, TransferOptions{Mode: mode, Source: made.SessionID, As: "Experiment"})
			if err != nil {
				t.Fatal(err)
			}
			selected, err := e.Store.ReadDefault(ctx, q.Workspace)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "clone" && (selected == nil || selected.ID != original.ID) {
				t.Fatal("copy changed the source default", selected)
			}
			if mode == "relocate" && selected != nil {
				t.Fatal("move retained the source default or selected its destination", selected)
			}
			if result.LocalName != "Experiment" || result.Workspace != q.Workspace {
				t.Fatal("lost explicit same-folder destination", result)
			}
			data, _ := json.Marshal(sessionRecord(t, e, result.Destination).Settings.Binding)
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
