package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/environment"
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func TestCopyCommandModes(t *testing.T) {
	for _, tt := range []struct {
		name          string
		move, running bool
	}{
		{"copy", false, false},
		{"copy-running", false, true},
		{"move", true, false},
		{"move-running", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			state, err := store.Open(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			resources := resource.Service{Home: state.Home}
			owner, err := resources.ConfigDirectory("test", t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = resources.CreateConfig(ctx, owner, resource.SetupOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err = resources.EditConfig(ctx, owner, resource.SetupOptions{Harness: harnessSetting("pi")}); err != nil {
				t.Fatal(err)
			}
			daemon := &dockertest.Daemon{}
			engine := &app.Engine{Store: state, Docker: docker.Runtime{Runner: daemon}, UID: 1000, GID: 1000}
			made, err := engine.Create(ctx, app.Request{Workspace: t.TempDir(), LocalName: "test", Sources: testConfigSources(engine.Store.Home, "test")})
			if err != nil {
				t.Fatal(err)
			}
			if tt.running {
				if _, err = engine.Start(ctx, made.Name, ""); err != nil {
					t.Fatal(err)
				}
			}
			source, err := state.Read(ctx, made.Name)
			if err != nil {
				t.Fatal(err)
			}
			destination, err := environment.Identify(t.TempDir(), "test")
			if err != nil {
				t.Fatal(err)
			}
			run := func(dry bool) (string, error) {
				profile := ""
				cmd := transferCommand(func(*cobra.Command) (*app.Engine, error) { return engine, nil }, &profile)
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&out)
				args := []string{made.Name, destination.Workspace}
				if tt.move {
					args = append(args, "--move")
				}
				if dry {
					args = append(args, "--dry-run")
				}
				cmd.SetArgs(args)
				err := cmd.ExecuteContext(ctx)
				return out.String(), err
			}
			if tt.running && !tt.move {
				if _, err = run(false); err == nil || !strings.Contains(err.Error(), "before copying") {
					t.Fatal("copy accepted a running source", err)
				}
				return
			}
			label := "copy"
			if tt.move {
				label += " --move"
			}
			if out, err := run(true); err != nil || !strings.Contains(out, "Would perform "+label+":") || !strings.Contains(out, "1. test") || !strings.Contains(out, owner.Root) || strings.Contains(out, "(fixed)") {
				t.Fatal("copy preview should show source label and path without type", out, err)
			}
			if _, err = state.Read(ctx, destination.Name); !os.IsNotExist(err) {
				t.Fatal("dry run created destination", err)
			}
			if out, err := run(false); err != nil || !strings.Contains(out, "Completed "+label+":") || !strings.Contains(out, "1. test") || !strings.Contains(out, owner.Root) || strings.Contains(out, "(fixed)") {
				t.Fatal("copy result should show source label and path without type", out, err)
			}
			copied, err := state.Read(ctx, destination.Name)
			if err != nil {
				t.Fatal(err)
			}
			if (copied.ID == source.ID) != tt.move || copied.ManualStart != (tt.move && tt.running) || copied.Action != label {
				t.Fatal("incorrect destination identity or lifetime", copied)
			}
			if _, err = state.Read(ctx, made.Name); tt.move && !os.IsNotExist(err) || !tt.move && err != nil {
				t.Fatal("incorrect source retention", err)
			}
			container, exists := daemon.Snapshot(destination.Name)
			if !exists || container.State.Running != (tt.move && tt.running) {
				t.Fatal("incorrect destination running state", container.State)
			}
		})
	}
}
