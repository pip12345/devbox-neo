package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"devbox/internal/environment"
)

func TestWorkspaceEditLeavesAppliedStateAndHistoryIntactUntilRecreation(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	before := sessionRecord(t, e, made.SessionID)
	if err := e.SetDefault(ctx, before); err != nil {
		t.Fatal(err)
	}
	history := filepath.Join(e.Store.Home, "sessions", before.Directory, "harnesses/pi/stores/home/sessions/history.json")
	write(t, history, "history")
	next := t.TempDir()
	calls := len(d.History())
	after, err := e.SetWorkspace(ctx, before.ID, "", next)
	if err != nil {
		t.Fatal(err)
	}
	if after.Directory != before.Directory || after.ID != before.ID || !reflect.DeepEqual(before.Applied, after.Applied) || len(d.History()) != calls {
		t.Fatal("workspace edit mutated runtime/storage")
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal(selected, err)
	}
	if selected, err := e.Store.ReadDefault(ctx, next); err != nil || selected != nil {
		t.Fatal("selected new default", selected, err)
	}
	if r, err := e.Locate(ctx, next, q.LocalName); err != nil || r.ID != before.ID {
		t.Fatal(r, err)
	}
	if _, err := e.Open(ctx, Request{Workspace: before.ID}); err != nil {
		t.Fatal("pending workspace edit blocked applied runtime", err)
	}
	if current := sessionRecord(t, e, before.ID); !reflect.DeepEqual(current.Applied, before.Applied) {
		t.Fatal("access applied a workspace edit")
	}
	if err := e.Stop(ctx, before.ID, "", false); err != nil {
		t.Fatal("edited settings prevented stopping old container", err)
	}
	status, err := e.Status(ctx, before.ID, "")
	if err != nil || status.Desired != environment.Recreate {
		t.Fatal(status, err)
	}
	if _, err := e.Recreate(ctx, Request{Workspace: before.ID}, false); err != nil {
		t.Fatal(err)
	}
	final := sessionRecord(t, e, before.ID)
	if final.Directory != before.Directory || final.Applied.SetupContainer == before.Applied.SetupContainer || final.Applied.Inputs.Container.Workspace != next {
		t.Fatal(final)
	}
	if data, err := os.ReadFile(history); err != nil || string(data) != "history" {
		t.Fatal("lost history", err)
	}
	if final.Applied.Creation.Name == final.Directory {
		t.Fatal("runtime and storage names were coupled")
	}
}

func TestRawJSONWorkspaceEditDoesNotInvalidateAppliedSnapshot(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	path, err := e.Store.RecordPath(r.Directory)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["identity"] != nil || fields["name"] != nil || fields["directory"] != nil || fields["settings"] == nil || fields["applied"] == nil {
		t.Fatal("record contains a storage alias or mixed ownership", string(data))
	}
	var settings map[string]any
	json.Unmarshal(fields["settings"], &settings)
	settings["workspace"] = filepath.Join(t.TempDir(), "missing")
	fields["settings"], _ = json.Marshal(settings)
	data, _ = json.Marshal(fields)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	after, err := e.Locate(ctx, r.ID, "")
	if err != nil || !reflect.DeepEqual(after.Applied, r.Applied) {
		t.Fatal("settings edit invalidated runtime", err)
	}
	if err := e.Stop(ctx, r.ID, "", false); err != nil {
		t.Fatal("missing desired workspace blocked runtime cleanup", err)
	}
}

func TestStorageDirectoryAndDockerNameAreNotIdentity(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	old := filepath.Join(e.Store.Home, "sessions", r.Directory)
	if err := os.Rename(old, filepath.Join(e.Store.Home, "sessions", "arbitrary-folder")); err != nil {
		t.Fatal(err)
	}
	r, err = e.Locate(ctx, r.ID, "")
	if err != nil || r.Directory != "arbitrary-folder" {
		t.Fatal(r, err)
	}
	// A Docker rename must not retarget the session to a different instance.
	c, ok := d.Snapshot(r.Applied.Creation.Name)
	if !ok {
		t.Fatal("missing fixture container")
	}
	d.Forget(r.Applied.Creation.Name)
	c.Name = "/an-independent-container-name"
	d.SetContainer(c)
	if err := e.Stop(ctx, r.ID, "", false); err != nil {
		t.Fatal("name matching still authorizes Docker operations", err)
	}
}
