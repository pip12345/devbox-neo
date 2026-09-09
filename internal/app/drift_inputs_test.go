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
			e, d, q := fixture(t)
			ctx := context.Background()
			result, err := e.Open(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			initial := record(t, e, result.Name)
			write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","network":"host","extra_env":["TOKEN=never-display-me"]}`)
			write(t, filepath.Join(e.Store.Home, "profiles/test/pi/new-file"), "new config")
			before, err := e.Status(ctx, result.Name, "")
			if err != nil || before.Desired != environment.Recreate {
				t.Fatal(before, err)
			}
			if !reflect.DeepEqual(record(t, e, result.Name).Inputs, initial.Inputs) {
				t.Fatal("status advanced the baseline")
			}
			if recovery {
				d.Forget(result.Name)
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
			applied := record(t, e, result.Name)
			if !reflect.DeepEqual(applied.Inputs.Image, initial.Inputs.Image) || !reflect.DeepEqual(applied.Inputs.Container, initial.Inputs.Container) {
				t.Fatal("open/recovery advanced pending creation inputs")
			}
			if reflect.DeepEqual(applied.Inputs.Runtime, initial.Inputs.Runtime) || applied.Applied.Runtime != applied.Inputs.Runtime.Fingerprint() {
				t.Fatal("runtime baseline did not advance with synchronization")
			}
			after, err := e.Status(ctx, result.Name, "")
			if err != nil || after.Desired != environment.Recreate {
				t.Fatal(after, err)
			}
			for _, inputChange := range after.PendingInputChanges {
				if inputChange.Scope == environment.RuntimeScope {
					t.Fatal("applied runtime input remained pending", inputChange)
				}
			}
			var output bytes.Buffer
			e.Streams.Err = &output
			if _, err := e.Open(ctx, q); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "network: default -> host") || !strings.Contains(output.String(), "environment variable TOKEN added") || strings.Contains(output.String(), "never-display-me") {
				t.Fatal("unclear or unsafe warning", output.String())
			}
			if _, err := e.Recreate(ctx, q, false); err != nil {
				t.Fatal(err)
			}
			final, err := e.Status(ctx, result.Name, "")
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
				write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","on_exit":"running"}`)
			}
			ctx := context.Background()
			result, err := e.Open(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			initial := record(t, e, result.Name)
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
			stored := record(t, e, result.Name)
			if stored.Applied.Runtime != initial.Applied.Runtime || !reflect.DeepEqual(stored.Inputs.Runtime, initial.Inputs.Runtime) {
				t.Fatal("uncommitted/deferred runtime baseline advanced")
			}
			d.Fail = nil
			view, err := e.Status(ctx, result.Name, "")
			if err != nil || view.Desired != environment.RuntimeSync || len(view.PendingInputChanges) == 0 {
				t.Fatal("pending config no longer explained", view, err)
			}
		})
	}
}

func TestFailedRecreationPreservesImageBaseline(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	result, err := e.Open(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	initial := record(t, e, result.Name)
	write(t, filepath.Join(e.Store.Home, "profiles/test/Dockerfile"), "FROM debian:bookworm-slim\n")
	view, err := e.Status(ctx, result.Name, "")
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
	stored := record(t, e, result.Name)
	if !reflect.DeepEqual(stored.Inputs, initial.Inputs) || stored.Applied != initial.Applied {
		t.Fatal("failed recreation advanced baseline")
	}
	d.Fail = nil
	if _, err := e.Recreate(ctx, q, false); err != nil {
		t.Fatal(err)
	}
	view, err = e.Status(ctx, result.Name, "")
	if err != nil || view.Desired != environment.NoChange || len(view.PendingInputChanges) != 0 {
		t.Fatal(view, err)
	}
}

func TestSessionRecordRequiresCompleteCurrentInputSnapshot(t *testing.T) {
	e, _, q := fixture(t)
	result, err := e.Open(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	original := record(t, e, result.Name)
	if original.Version != 2 {
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
		{"missing inputs", func(r *store.Record) { r.Inputs = environment.Inputs{} }},
		{"changed baseline", func(r *store.Record) { r.Inputs.Container.Network = "host" }},
		{"missing env map", func(r *store.Record) { r.Inputs.Container.Env = nil }},
		{"bad hash", func(r *store.Record) { r.Inputs.Runtime.Assets = "invalid" }},
		{"wrong source kind", func(r *store.Record) { r.Inputs.Image.Definition.Source = "relative/path" }},
		{"unredacted raw env", func(r *store.Record) { r.Inputs.Container.RawArgs = []string{"--env=TOKEN=private-value"} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var r store.Record
			if err := json.Unmarshal(data, &r); err != nil {
				t.Fatal(err)
			}
			tt.edit(&r)
			err := r.Validate(r.Identity.Name)
			if err == nil || strings.Contains(err.Error(), "private-value") {
				t.Fatal("accepted or exposed invalid baseline", err)
			}
		})
	}
}
