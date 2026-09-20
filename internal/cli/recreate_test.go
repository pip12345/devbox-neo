package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func TestRecreateProjectDirectoryCLI(t *testing.T) {
	ctx := context.Background()
	state, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resources := resource.Service{Home: state.Home}
	owner, err := resources.Profile("test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resources.Create(ctx, owner, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = resources.Init(ctx, owner, resource.InitOptions{Harness: "pi"}); err != nil {
		t.Fatal(err)
	}
	workspace, oldDir := t.TempDir(), t.TempDir()
	if err = os.WriteFile(filepath.Join(oldDir, "config.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	engine := &app.Engine{Store: state, Docker: docker.Runtime{Runner: &dockertest.Daemon{}}, UID: 1000, GID: 1000}
	made, err := engine.Create(ctx, app.Request{Workspace: workspace, Profile: "test", ProjectDir: oldDir})
	if err != nil {
		t.Fatal(err)
	}
	before, err := state.Read(ctx, made.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(oldDir); err != nil {
		t.Fatal(err)
	}
	defaultDir := filepath.Join(workspace, ".devbox")
	if err = os.Mkdir(defaultDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(defaultDir, "config.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	profile := ""
	cmd := recreateCommand(func(*cobra.Command) (*app.Engine, error) { return engine, nil }, &profile)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{made.Name, "--project-dir", defaultDir})
	if err = cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	after, err := state.Read(ctx, made.Name)
	if err != nil || after.ID != before.ID || after.Identity.ProjectDir != "" || after.Sources[1].Path != defaultDir {
		t.Fatal(after.Identity, after.Sources, err)
	}
}

func TestRecreateRejectsInvalidProjectDirectoryFlagsBeforeInitialization(t *testing.T) {
	for _, args := range [][]string{
		{"target", "--project-dir="},
		{"--all", "--project-dir", "/config"},
	} {
		profile := ""
		cmd := recreateCommand(func(*cobra.Command) (*app.Engine, error) {
			t.Fatal("invalid selection initialized the engine")
			return nil, nil
		}, &profile)
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--project-dir") {
			t.Fatal(args, err)
		}
	}
}
