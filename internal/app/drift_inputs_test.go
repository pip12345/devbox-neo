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

func TestDriftBaselineTracksAppliedNotMerelyDesiredInputs(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "recovery"}[recovery], func(t *testing.T) {
			e, _, q := fixture(t)
			ctx := context.Background()
			result, err := createAndOpen(ctx, e, q)
			if err != nil {
				t.Fatal(err)
			}
			initial := sessionRecord(t, e, result.SessionID)
			write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","network":"host","env":["TOKEN=never-display-me"]}`)
			write(t, filepath.Join(e.Store.Home, "profiles/test/pi/new-file"), "new config")
			before, err := e.Status(ctx, result.SessionID, "")
			if err != nil || before.Desired != environment.Recreate {
				t.Fatal(before, err)
			}
			if !reflect.DeepEqual(sessionRecord(t, e, result.SessionID).Applied.Inputs, initial.Applied.Inputs) {
				t.Fatal("status advanced the baseline")
			}
			if recovery {
				forgetSession(t, e, result.SessionID)
			}
			result, err = e.Open(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) == 0 {
				t.Fatal("creation reasons missing")
			}
			want := environment.Report{PendingInputChanges: before.PendingInputChanges}.PendingCreationChanges()
			if !reflect.DeepEqual(result.Diagnostics[0].PendingInputChanges, want) {
				t.Fatal("status/open disagree", before.PendingInputChanges, result.Diagnostics)
			}
			applied := sessionRecord(t, e, result.SessionID)
			if !reflect.DeepEqual(applied.Applied.Inputs.Image, initial.Applied.Inputs.Image) || !reflect.DeepEqual(applied.Applied.Inputs.Container, initial.Applied.Inputs.Container) {
				t.Fatal("open/recovery advanced pending creation inputs")
			}
			if reflect.DeepEqual(applied.Applied.Inputs.Runtime, initial.Applied.Inputs.Runtime) || applied.Applied.Fingerprints.Runtime != applied.Applied.Inputs.Runtime.Fingerprint() {
				t.Fatal("runtime baseline did not advance with synchronization")
			}
			after, err := e.Status(ctx, result.SessionID, "")
			if err != nil || after.Desired != environment.Recreate {
				t.Fatal(after, err)
			}
			for _, inputChange := range after.PendingInputChanges {
				if inputChange.Scope == environment.RuntimeScope {
					t.Fatal("applied runtime input remained pending", inputChange)
				}
			}
			var emitted []Diagnostic
			e.OnDiagnostic = func(diagnostic Diagnostic) { emitted = append(emitted, diagnostic) }
			if _, err := e.Open(ctx, q); err != nil {
				t.Fatal(err)
			}
			if len(emitted) != 1 || !reflect.DeepEqual(emitted[0].PendingInputChanges, want) {
				t.Fatal("callback lost creation reasons", emitted)
			}
			encoded, err := json.Marshal(emitted)
			if err != nil || bytes.Contains(encoded, []byte("never-display-me")) {
				t.Fatal("unsafe diagnostic", string(encoded), err)
			}
			if _, err := e.Recreate(ctx, q, false); err != nil {
				t.Fatal(err)
			}
			final, err := e.Status(ctx, result.SessionID, "")
			if err != nil || final.Desired != environment.NoChange || len(final.PendingInputChanges) != 0 {
				t.Fatal("recreation did not commit baseline", final, err)
			}
		})
	}
}

func TestDeferredOrFailedOpenDoesNotAdvanceRuntimeBaseline(t *testing.T) {
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
			result, err = e.Open(ctx, q)
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
	write(t, filepath.Join(e.Store.Home, "profiles/test/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\n")
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
	if _, err := e.Recreate(ctx, q, false); err == nil {
		t.Fatal("expected failure")
	}
	stored := sessionRecord(t, e, result.SessionID)
	if !reflect.DeepEqual(stored.Applied.Inputs, initial.Applied.Inputs) || stored.Applied.Fingerprints != initial.Applied.Fingerprints {
		t.Fatal("failed recreation advanced baseline")
	}
	d.Fail = nil
	if _, err := e.Recreate(ctx, q, false); err != nil {
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
