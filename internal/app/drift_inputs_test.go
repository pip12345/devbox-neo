package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/environment"
	"devbox/internal/store"
)

func TestDriftBaselineTracksExplicitApplication(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	initial := sessionRecord(t, e, made.SessionID)
	write(t, filepath.Join(q.Sources[0].Path, "config.json"), `{"harness":"pi","network":"host","env":["TOKEN=never-display-me"]}`)
	write(t, filepath.Join(q.Sources[0].Path, "pi/new-file"), "new config")
	before, err := e.Status(ctx, made.SessionID, "")
	if err != nil || before.Desired != environment.Recreate {
		t.Fatal(before, err)
	}
	opened, err := e.Open(ctx, openRequest(q))
	if err != nil || len(opened.Diagnostics) != 0 {
		t.Fatal(opened, err)
	}
	if current := sessionRecord(t, e, made.SessionID); !reflect.DeepEqual(current.Applied.Inputs, initial.Applied.Inputs) {
		t.Fatal("inspection/access advanced applied inputs")
	}
	after, err := e.Status(ctx, made.SessionID, "")
	if err != nil || !reflect.DeepEqual(before.PendingInputChanges, after.PendingInputChanges) {
		t.Fatal("access hid pending changes", err)
	}
	encoded, err := json.Marshal(after)
	if err != nil || bytes.Contains(encoded, []byte("never-display-me")) {
		t.Fatal("status exposed env values", err)
	}
	if _, err := e.Recreate(ctx, recreateRequest(q), false); err != nil {
		t.Fatal(err)
	}
	final, err := e.Status(ctx, made.SessionID, "")
	if err != nil || final.Desired != environment.NoChange || len(final.PendingInputChanges) != 0 {
		t.Fatal("apply did not commit baseline", final, err)
	}
}

func TestAccessDoesNotAdvanceRuntimeBaseline(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed", true: "deferred"}[running], func(t *testing.T) {
			e, d, q := fixture(t)
			if running {
				write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi"}`)
			}
			ctx := context.Background()
			result, err := createAndOpen(ctx, e, q)
			if err != nil {
				t.Fatal(err)
			}
			if running {
				if _, err = e.Start(ctx, result.SessionID, ""); err != nil {
					t.Fatal(err)
				}
			}
			initial := sessionRecord(t, e, result.SessionID)
			write(t, filepath.Join(e.Store.Home, "profiles/test/pi/new-file"), "new config")
			if !running {
				d.Fail = func(args []string) error {
					if args[0] == "start" {
						return errors.New("injected start failure")
					}
					return nil
				}
			}
			result, err = e.Open(ctx, openRequest(q))
			if !running && err == nil || running && err != nil {
				t.Fatal(err)
			}
			stored := sessionRecord(t, e, result.SessionID)
			if stored.Applied.Fingerprints.Runtime != initial.Applied.Fingerprints.Runtime || !reflect.DeepEqual(stored.Applied.Inputs.Runtime, initial.Applied.Inputs.Runtime) {
				t.Fatal("uncommitted/deferred runtime baseline advanced")
			}
			d.Fail = nil
			view, err := e.Status(ctx, result.SessionID, "")
			if err != nil || view.Desired != environment.RuntimeSync || len(view.PendingInputChanges) == 0 {
				t.Fatal("pending config no longer explained", view, err)
			}
		})
	}
}

func TestFailedRecreationPreservesImageBaseline(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	initial := sessionRecord(t, e, result.SessionID)
	write(t, filepath.Join(e.Store.Home, "profiles/test/docker/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\n")
	view, err := e.Status(ctx, result.SessionID, "")
	if err != nil || view.Desired != environment.RebuildAndRecreate {
		t.Fatal(view, err)
	}
	d.Fail = func(args []string) error {
		if args[0] == "create" {
			return errors.New("injected create failure")
		}
		return nil
	}
	if _, err := e.Recreate(ctx, recreateRequest(q), false); err == nil {
		t.Fatal("expected failure")
	}
	stored := sessionRecord(t, e, result.SessionID)
	if !reflect.DeepEqual(stored.Applied.Inputs, initial.Applied.Inputs) || stored.Applied.Fingerprints != initial.Applied.Fingerprints {
		t.Fatal("failed recreation advanced baseline")
	}
	d.Fail = nil
	lock, err := e.Store.Lock(ctx, stored.Directory, stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.planSessionDeletion(ctx, []*store.Locked{lock}, true)
	lock.Close()
	if err != nil {
		t.Fatal("failed build left a wrong image-tag association blocking deletion", err)
	}
	if _, err := e.Recreate(ctx, recreateRequest(q), false); err != nil {
		t.Fatal(err)
	}
	view, err = e.Status(ctx, result.SessionID, "")
	if err != nil || view.Desired != environment.NoChange || len(view.PendingInputChanges) != 0 {
		t.Fatal(view, err)
	}
}

func TestSessionRecordRequiresCompleteCurrentInputSnapshot(t *testing.T) {
	e, _, q := fixture(t)
	result, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	original := sessionRecord(t, e, result.SessionID)
	if original.Version != store.RecordVersion {
		t.Fatal("record format not updated")
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		edit func(*store.Record)
	}{
		{"old version", func(r *store.Record) { r.Version = 1 }},
		{"missing inputs", func(r *store.Record) { r.Applied.Inputs = environment.Inputs{} }},
		{"changed baseline", func(r *store.Record) { r.Applied.Inputs.Container.Network = "host" }},
		{"missing env map", func(r *store.Record) { r.Applied.Inputs.Container.Env = nil }},
		{"bad hash", func(r *store.Record) { r.Applied.Inputs.Runtime.Assets = "invalid" }},
		{"wrong source kind", func(r *store.Record) { r.Applied.Inputs.Image.Definition.Source = "relative/path" }},
		{"unredacted raw env", func(r *store.Record) { r.Applied.Inputs.Container.RawArgs = []string{"--env=TOKEN=private-value"} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var r store.Record
			if err := json.Unmarshal(data, &r); err != nil {
				t.Fatal(err)
			}
			tt.edit(&r)
			err := r.Validate(r.Directory)
			if err == nil || strings.Contains(err.Error(), "private-value") {
				t.Fatal("accepted or exposed invalid baseline", err)
			}
		})
	}
}
