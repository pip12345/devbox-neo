package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/app"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func TestRecreateUsesRecordedExplicitSources(t *testing.T) {
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
	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, ".devbox")
	if err = os.Mkdir(projectDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(projectDir, "config.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	engine := &app.Engine{Store: state, Docker: docker.Runtime{Runner: &dockertest.Daemon{}}, UID: 1000, GID: 1000}
	made, err := engine.Create(ctx, app.CreateRequest{Workspace: workspace, LocalName: "test", Sources: append(testConfigSources(engine.Store.Home, "test"), config.Reference{Label: "overlay", Kind: config.ReferenceFixed, Path: projectDir})})
	if err != nil {
		t.Fatal(err)
	}
	before, err := state.Find(ctx, made.SessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(projectDir, "config.json"), []byte(`{"ports":["8080:80"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	profile := ""
	cmd := recreateCommand(func(*cobra.Command) (*app.Engine, error) { return engine, nil }, &profile)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{before.Directory})
	if err = cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	after, err := state.Find(ctx, made.SessionID, nil)
	if err != nil || after.ID != before.ID || after.Settings.Binding != before.Settings.Binding || after.Settings.Sources[1].Path != projectDir || len(after.Applied.Creation.Ports) != 1 || after.Applied.Creation.Ports[0] != "8080:80" {
		t.Fatal(after.Settings.Binding, after.Settings.Sources, err)
	}
}

func TestRecreateRejectsInvalidTargetsBeforeInitialization(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--all", "target"},
	} {
		profile := ""
		cmd := recreateCommand(func(*cobra.Command) (*app.Engine, error) {
			t.Fatal("invalid selection initialized the engine")
			return nil, nil
		}, &profile)
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatal(args, err)
		}
	}
}
