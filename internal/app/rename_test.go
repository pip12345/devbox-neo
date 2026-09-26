package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"devbox/internal/environment"
)

func TestRenamePreservesSessionAndReplacesContainer(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "running"}[running], func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			created, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if running {
				if _, err := e.Start(ctx, created.Name, ""); err != nil {
					t.Fatal(err)
				}
			}
			before := record(t, e, created.Name)
			if err := e.SetDefault(ctx, before); err != nil {
				t.Fatal(err)
			}
			history := "harnesses/pi/stores/home/sessions/history.json"
			write(t, filepath.Join(e.Store.Home, "sessions", created.Name, history), "history")
			preview, err := e.Rename(ctx, q.Workspace, q.LocalName, "Renamed", true)
			if err != nil || !preview.DryRun || preview.SessionID != before.ID {
				t.Fatal(preview, err)
			}
			if _, err := e.Store.Read(ctx, preview.Destination); !os.IsNotExist(err) {
				t.Fatal("preview created destination", err)
			}
			if after := record(t, e, created.Name); !reflect.DeepEqual(before, after) {
				t.Fatal("preview changed source")
			}
			result, err := e.Rename(ctx, q.Workspace, "", "Renamed", false)
			if err != nil {
				t.Fatal(err)
			}
			after := record(t, e, result.Destination)
			if after.ID != before.ID || !after.Created.Equal(before.Created) || after.Identity.Workspace != q.Workspace || after.Identity.LocalName != "Renamed" || !reflect.DeepEqual(after.Sources, before.Sources) || after.SetupContainer == before.SetupContainer {
				t.Fatal("rename lost durable identity or failed to replace the container", after)
			}
			data, err := os.ReadFile(filepath.Join(e.Store.Home, "sessions", result.Destination, history))
			if err != nil || string(data) != "history" {
				t.Fatal("lost harness history", err)
			}
			container, exists := d.Snapshot(result.Destination)
			if !exists || container.State.Running != running || after.ManualStart != running {
				t.Fatal("running intent changed", container, after.ManualStart)
			}
			if _, exists := d.Snapshot(created.Name); exists {
				t.Fatal("old container remains")
			}
			if _, err := e.Store.Read(ctx, created.Name); !os.IsNotExist(err) {
				t.Fatal("old record remains", err)
			}
			if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
				t.Fatal("move semantics did not clear the source default", selected, err)
			}
		})
	}
}

func TestRenameRejectsInvalidSameAndOccupiedNames(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	original := record(t, e, created.Name)
	other := q
	other.LocalName = "Occupied"
	if _, err := e.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"", "bad/name", q.LocalName, "Occupied"} {
		if _, err := e.Rename(ctx, created.Name, "", to, false); err == nil {
			t.Fatal("accepted invalid rename", to)
		}
	}
	if after := record(t, e, created.Name); !reflect.DeepEqual(original, after) {
		t.Fatal("rejected rename changed source")
	}
}

func TestRenameRetriesExistingTransferJournal(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	original := record(t, e, created.Name)
	d.Fail = func(args []string) error {
		if len(args) > 0 && args[0] == "create" {
			return errors.New("injected create failure")
		}
		return nil
	}
	if _, err := e.Rename(ctx, created.Name, "", "Renamed", false); err == nil {
		t.Fatal("missing injected failure")
	}
	if pending, err := e.Store.Pending(created.Name); err != nil || pending == nil {
		t.Fatal("missing retry journal", pending, err)
	}
	if _, err := e.Rename(ctx, created.Name, "", "Different", false); err == nil {
		t.Fatal("retry changed destination")
	}
	d.Fail = nil
	result, err := e.Rename(ctx, created.Name, "", "Renamed", false)
	if err != nil || result.SessionID != original.ID || result.Destination != environment.ContainerName(q.Workspace, "Renamed") {
		t.Fatal(result, err)
	}
	if pending, err := e.Store.Pending(result.Destination); err != nil || pending != nil {
		t.Fatal("journal remains after successful retry", pending, err)
	}
}
