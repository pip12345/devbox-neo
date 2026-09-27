package app

import (
	"context"
	"reflect"
	"testing"
)

func TestRenameOnlyEditsSettings(t *testing.T) {
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
	calls := len(d.History())
	preview, err := e.Rename(ctx, made.SessionID, "", "Renamed", true)
	if err != nil || !preview.DryRun || preview.SessionID != before.ID {
		t.Fatal(preview, err)
	}
	if after := sessionRecord(t, e, before.ID); !reflect.DeepEqual(before, after) {
		t.Fatal("preview changed state")
	}
	result, err := e.Rename(ctx, made.SessionID, "", "Renamed", false)
	if err != nil || result.PreviousName != q.LocalName {
		t.Fatal(result, err)
	}
	after := sessionRecord(t, e, before.ID)
	if after.Settings.LocalName != "Renamed" || after.Directory != before.Directory || !reflect.DeepEqual(before.Applied, after.Applied) || len(d.History()) != calls {
		t.Fatal("rename changed storage/runtime", after)
	}
	selected, err := e.Store.ReadDefault(ctx, q.Workspace)
	if err != nil || selected == nil || selected.ID != before.ID {
		t.Fatal("rename changed default", selected, err)
	}
	if _, err := e.Locate(ctx, q.Workspace, q.LocalName); err == nil {
		t.Fatal("old name remained selectable")
	}
	if got, err := e.Locate(ctx, q.Workspace, "Renamed"); err != nil || got.ID != before.ID {
		t.Fatal(got, err)
	}
	// Reusing the old label allocates separate storage and runtime names.
	other, err := e.Create(ctx, q)
	if err != nil || other.SessionID == before.ID {
		t.Fatal(other, err)
	}
	second := sessionRecord(t, e, other.SessionID)
	if second.Directory == before.Directory || second.Applied.Creation.Name == before.Applied.Creation.Name {
		t.Fatal("reused label collided")
	}
}

func TestRenameRejectsInvalidAndOccupiedNames(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	before := sessionRecord(t, e, made.SessionID)
	other := q
	other.LocalName = "Occupied"
	if _, err := e.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"", "bad/name", "Occupied"} {
		if _, err := e.Rename(ctx, made.SessionID, "", to, false); err == nil {
			t.Fatal("accepted invalid name", to)
		}
	}
	if _, err := e.Rename(ctx, made.SessionID, "", q.LocalName, false); err != nil {
		t.Fatal("same-name edit should be a no-op", err)
	}
	if after := sessionRecord(t, e, made.SessionID); !reflect.DeepEqual(before, after) {
		t.Fatal("failed/no-op rename changed state")
	}
}
