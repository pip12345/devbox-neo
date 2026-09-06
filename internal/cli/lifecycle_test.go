package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func TestContainerAndSessionCLIUseSeparateDeletionContracts(t *testing.T) {
	ctx := context.Background()
	state, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resources := resource.Service{Home: state.Home}
	owner, _ := resources.Profile("test")
	resources.Create(ctx, owner, "")
	resources.Init(ctx, owner, resource.InitOptions{Harness: "pi"})
	daemon := &dockertest.Daemon{}
	engine := &app.Engine{Store: state, Docker: docker.Runtime{Runner: daemon}, UID: 1000, GID: 1000}
	result, err := engine.Open(ctx, app.Request{Workspace: t.TempDir(), Profile: "test"})
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		root := &cobra.Command{Use: "devbox-neo", SilenceUsage: true, SilenceErrors: true}
		profile := ""
		factory := func(cmd *cobra.Command) (*app.Engine, error) {
			engine.Streams.Out = cmd.OutOrStdout()
			engine.Streams.Err = cmd.ErrOrStderr()
			return engine, nil
		}
		root.AddCommand(containerCommands(factory, &profile)...)
		root.AddCommand(sessionCommands(factory, &profile))
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		err := root.Execute()
		return out.String(), err
	}
	for _, args := range [][]string{{"list", "--json"}, {"status", result.Name, "--json"}, {"session", "show", result.Name, "--json"}, {"network", "env", result.Name, "--get", "DEVBOX_HOST"}, {"logs", result.Name}} {
		if out, err := run(args...); err != nil || out == "" {
			t.Fatal(args, out, err)
		}
	}
	destination := t.TempDir()
	if out, err := run("session", "clone", result.Name, destination, "--dry-run", "--json"); err != nil || !strings.Contains(out, `"dry_run":true`) {
		t.Fatal(out, err)
	}
	if out, err := run("session", "clone", result.Name, destination, "--json"); err != nil || !strings.Contains(out, `"mode":"clone"`) {
		t.Fatal(out, err)
	}
	if _, err := run("session", "relocate", result.Name, "--from", "test"); err == nil {
		t.Fatal("incomplete slot flags accepted")
	}
	if _, err = run("session", "delete", result.Name); err == nil {
		t.Fatal("session deletion bypassed container")
	}
	if _, err = run("session", "delete", "--all"); err == nil {
		t.Fatal("session delete exposed a bulk flag")
	}
	if out, err := run("delete", result.Name); err != nil || !strings.Contains(out, "retained") {
		t.Fatal(out, err)
	}
	if out, err := run("session", "prune", "--orphaned", "--dry-run"); err != nil || !strings.Contains(out, "Would delete") {
		t.Fatal(out, err)
	}
	if _, err = run("session", "delete", result.Name); err != nil {
		t.Fatal(err)
	}
}
func TestRemovedFullDockerfileIsNotAnInitChoice(t *testing.T) {
	home := t.TempDir()
	resourceCLI(t, home, "profile", "create", "test")
	if _, err := resourceCLI(t, home, "profile", "init", "test", "--harness", "pi", "--artifact", "Dockerfile.full"); err == nil {
		t.Fatal("removed full override was seeded")
	}
}
