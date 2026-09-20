package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/docker"
)

func TestRecreateChangesAndClearsProjectDirectoryWithoutLosingState(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	oldDir, newDir := t.TempDir(), t.TempDir()
	defaultDir := filepath.Join(q.Workspace, ".devbox")
	write(t, filepath.Join(oldDir, "config.json"), `{"env":["SOURCE=old"]}`)
	write(t, filepath.Join(newDir, "config.json"), `{"env":["SOURCE=new"],"ports":["8080:80"]}`)
	write(t, filepath.Join(defaultDir, "config.json"), `{"env":["SOURCE=workspace"]}`)
	q.ProjectDir = oldDir
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(ctx, made.Name, ""); err != nil {
		t.Fatal(err)
	}
	before := record(t, e, made.Name)
	history := filepath.Join(e.Store.Home, "sessions", made.Name, "harnesses/pi/stores/home/history/keep")
	write(t, history, "saved conversation")
	// The old source is not needed when an exact saved target supplies a replacement.
	if err = os.RemoveAll(oldDir); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ path, want string }{
		{newDir, newDir},
		{"", newDir},
		{defaultDir, ""},
		{"", ""},
	} {
		if _, err = e.Recreate(ctx, Request{Workspace: made.Name, ProjectDir: step.path}, false); err != nil {
			t.Fatal(step, err)
		}
		after := record(t, e, made.Name)
		if after.ID != before.ID || after.Identity.Name != before.Identity.Name || after.Identity.Slot != before.Identity.Slot || after.Identity.ProjectDir != step.want || !after.ManualStart {
			t.Fatal(step, after.Identity, after.ManualStart)
		}
		root := step.want
		if root == "" {
			root = defaultDir
		}
		if len(after.Sources) != 2 || after.Sources[1].Path != root || len(after.EnvSources) != 1 || after.EnvSources[0].Path != filepath.Join(root, "config.json") {
			t.Fatal("source binding was not updated consistently", after.Sources, after.EnvSources)
		}
		live, exists := d.Snapshot(made.Name)
		if !exists || !live.State.Running || live.HostConfig.RestartPolicy.Name != "unless-stopped" {
			t.Fatal("lost manual running intent", live)
		}
		data, err := os.ReadFile(history)
		if err != nil || string(data) != "saved conversation" {
			t.Fatal("lost saved history", err)
		}
	}
	// Clearing restores ordinary destination-project behavior for future copies.
	if err = e.Stop(ctx, made.Name, "", false); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	write(t, filepath.Join(destination, ".devbox/config.json"), `{}`)
	copied, err := e.Transfer(ctx, TransferOptions{Mode: "clone", Source: made.Name, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	copyRecord := record(t, e, copied.Destination)
	if copyRecord.Identity.ProjectDir != "" || copyRecord.Sources[1].Path != filepath.Join(destination, ".devbox") {
		t.Fatal("cleared source remained pinned to old workspace", copyRecord.Identity, copyRecord.Sources)
	}
}

func TestFailedProjectRebindingKeepsRecordedSelection(t *testing.T) {
	for _, failure := range []string{"missing", "invalid", "inheritance", "build", "setup"} {
		t.Run(failure, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			oldDir, newDir := t.TempDir(), t.TempDir()
			write(t, filepath.Join(oldDir, "config.json"), `{}`)
			q.ProjectDir = oldDir
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			before := record(t, e, made.Name)
			history := filepath.Join(e.Store.Home, "sessions", made.Name, "harnesses/pi/stores/home/history/keep")
			write(t, history, "saved conversation")
			write(t, filepath.Join(newDir, "config.json"), `{}`)
			switch failure {
			case "missing":
				os.Remove(filepath.Join(newDir, "config.json"))
			case "invalid":
				write(t, filepath.Join(newDir, "config.json"), "broken")
			case "inheritance":
				write(t, filepath.Join(newDir, "config.json"), `{"inherit":false,"harness":"pi"}`)
			case "build":
				write(t, filepath.Join(newDir, "Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\nRUN false\n")
				d.Fail = func(args []string) error {
					if args[0] == "build" {
						return errors.New("new build failed")
					}
					return nil
				}
			case "setup":
				write(t, filepath.Join(newDir, "setup.sh"), "fail new setup")
				d.Attached = func(_ context.Context, c docker.Command) error {
					if c.Stdin != nil {
						data, _ := io.ReadAll(c.Stdin)
						if strings.Contains(string(data), "fail new setup") {
							return errors.New("new setup failed")
						}
					}
					return nil
				}
			}
			calls := len(d.History())
			if _, err = e.Recreate(ctx, Request{Workspace: made.Name, ProjectDir: newDir}, false); err == nil {
				t.Fatal("failed replacement succeeded")
			}
			after := record(t, e, made.Name)
			if after.ID != before.ID || after.Identity != before.Identity || !reflect.DeepEqual(after.Sources, before.Sources) {
				t.Fatal("failed recreation committed its source binding", before.Identity, after.Identity, after.Sources)
			}
			data, err := os.ReadFile(history)
			if err != nil || string(data) != "saved conversation" {
				t.Fatal("failed recreation lost saved history", err)
			}
			if failure == "missing" || failure == "invalid" || failure == "inheritance" {
				if len(d.History()) != calls {
					t.Fatal("invalid replacement touched Docker")
				}
			}
		})
	}
}

func TestProjectRebindingRejectsProfileOnlyAndBulkTargets(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.json"), `{}`)
	calls := len(d.History())
	if _, err = e.Recreate(ctx, Request{Workspace: made.Name, ProjectDir: dir}, false); err == nil {
		t.Fatal("added a project to a profile-only identity")
	}
	if _, err = e.RecreateAll(ctx, false, Request{ProjectDir: dir}); err == nil {
		t.Fatal("bulk source replacement accepted")
	}
	if len(d.History()) != calls {
		t.Fatal("invalid source replacement touched Docker")
	}
}

func TestExplicitConventionalProjectDirectoryIsNotAnOverride(t *testing.T) {
	e, _, q := fixture(t)
	q.ProjectDir = filepath.Join(q.Workspace, ".devbox")
	write(t, filepath.Join(q.ProjectDir, "config.json"), `{}`)
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if r := record(t, e, made.Name); r.Identity.ProjectDir != "" || r.Sources[1].Path != q.ProjectDir {
		t.Fatal(r.Identity, r.Sources)
	}
}
