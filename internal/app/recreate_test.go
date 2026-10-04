package app

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/environment"
)

func TestRecreateChoosesMinimumRequiredWork(t *testing.T) {
	for _, kind := range []string{"runtime", "container", "image", "force-container", "force-image", "pruned-image", "missing-container"} {
		t.Run(kind, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			old := sessionRecord(t, e, made.SessionID)
			builds, creates := count(d, "build"), count(d, "create")
			switch kind {
			case "runtime":
				write(t, filepath.Join(q.Sources[0].Path, "pi/new-file"), "new")
			case "container":
				write(t, filepath.Join(q.Sources[0].Path, "config.json"), `{"harness":"pi","network":"host"}`)
			case "image":
				write(t, filepath.Join(q.Sources[0].Path, "docker/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\n")
			case "force-container":
				q.ForceContainer = true
			case "pruned-image":
				delete(d.Images, old.Applied.ImageID)
				delete(d.Images, old.Applied.ImageTag)
			case "missing-container":
				forgetSession(t, e, made.SessionID)
			}
			if _, err := e.Recreate(ctx, q, kind == "force-image"); err != nil {
				t.Fatal(err)
			}
			wantReplacement := kind != "runtime" && kind != "pruned-image"
			wantBuild := kind == "image" || kind == "force-image"
			current := sessionRecord(t, e, made.SessionID)
			if (count(d, "create") > creates) != wantReplacement || (count(d, "build") > builds) != wantBuild {
				t.Fatal("wrong application level", d.History())
			}
			if current.ID != old.ID || (current.Applied.SetupContainer != old.Applied.SetupContainer) != wantReplacement {
				t.Fatal("wrong saved/runtime identity")
			}
			if kind == "force-image" {
				for _, args := range d.History() {
					if args[0] == "build" && slices.Contains(args, "--no-cache") {
						return
					}
				}
				t.Fatal("forced image build used cache")
			}
		})
	}
}

func TestRuntimeApplyCapturesInputsAndPreservesLifetime(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "running"}[running], func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if running {
				if _, err := e.Start(ctx, made.SessionID, ""); err != nil {
					t.Fatal(err)
				}
			}
			old := sessionRecord(t, e, made.SessionID)
			source := q.Sources[0].Path
			write(t, filepath.Join(source, "config.json"), `{"harness":"pi","harness_args":["--captured"],"shell":["sh"]}`)
			write(t, filepath.Join(source, "before-open.sh"), "echo captured")
			e.OnDiagnostic = func(Diagnostic) {
				write(t, filepath.Join(source, "config.json"), "broken after capture")
				write(t, filepath.Join(source, "before-open.sh"), "echo must-not-apply")
			}
			if _, err := e.Recreate(ctx, q, false); err != nil {
				t.Fatal(err)
			}
			current := sessionRecord(t, e, made.SessionID)
			if !reflect.DeepEqual(current.Applied.Inputs.Image, old.Applied.Inputs.Image) || !reflect.DeepEqual(current.Applied.Inputs.Container, old.Applied.Inputs.Container) || current.Applied.SetupContainer != old.Applied.SetupContainer {
				t.Fatal("runtime apply changed creation contract")
			}
			if !slices.Contains(current.Applied.Launch.Args, "--captured") || !slices.Equal(current.Applied.Launch.Shell, []string{"sh"}) {
				t.Fatal("launch did not use captured inputs")
			}
			path := docker.OpenHookPath(environment.Digest([]byte("echo captured")))
			if string(d.Hooks[current.Applied.SetupContainer][path]) != "echo captured" {
				t.Fatal("hook was reread")
			}
			c, _ := sessionSnapshot(t, e, made.SessionID)
			if c.State.Running != running || current.Settings.ManualStart != running {
				t.Fatal("apply changed lifetime")
			}
			if _, err := e.Open(ctx, q); err != nil {
				t.Fatal("access read broken desired config", err)
			}
		})
	}
}

func TestAppliedRuntimeStopFailureSuggestsStopRatherThanReapplication(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(q.Sources[0].Path, "config.json"), `{"harness":"pi","harness_args":["--applied"]}`)
	d.Fail = func(args []string) error {
		if args[0] == "stop" {
			return errors.New("stop failed")
		}
		return nil
	}
	var failure *commanderror.Error
	if _, err := e.Recreate(ctx, q, false); !errors.As(err, &failure) || failure.Code != "runtime_apply_failed" {
		t.Fatal(err)
	}
	if !slices.Equal(failure.Next[len(failure.Next)-1].Command, []string{"dbx", "stop", made.SessionID}) {
		t.Fatal("wrong lifetime repair", failure.Next)
	}
	if !slices.Contains(sessionRecord(t, e, made.SessionID).Applied.Launch.Args, "--applied") {
		t.Fatal("successful apply was not recorded")
	}
}

func TestFailedRuntimeApplyRetainsAppliedLaunchAndHooks(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	source := q.Sources[0].Path
	write(t, filepath.Join(source, "before-open.sh"), "echo original")
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	old := sessionRecord(t, e, made.SessionID)
	write(t, filepath.Join(source, "before-open.sh"), "echo replacement")
	write(t, filepath.Join(source, "config.json"), `{"harness":"pi","harness_args":["--new"]}`)
	d.Fail = func(args []string) error {
		if args[len(args)-1] == "dbx-publish-hooks" {
			return errors.New("publish failed")
		}
		return nil
	}
	if _, err := e.Recreate(ctx, q, false); err == nil {
		t.Fatal("apply failure hidden")
	}
	current := sessionRecord(t, e, made.SessionID)
	if !reflect.DeepEqual(current.Applied, old.Applied) {
		t.Fatal("failed apply advanced the record")
	}
	d.Fail = nil
	if _, err := e.Open(ctx, q); err != nil {
		t.Fatal("failed apply lost prior hook copies", err)
	}
	for _, args := range d.History() {
		if args[0] == "exec" && strings.HasSuffix(args[len(args)-1], environment.Digest([]byte("echo replacement"))+".sh") {
			t.Fatal("uncommitted hook executed")
		}
	}
}
