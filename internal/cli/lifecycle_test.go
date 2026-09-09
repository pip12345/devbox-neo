package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
		root.PersistentFlags().StringVar(&profile, "profile", "", "Select a profile")
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
	for _, args := range [][]string{{"list", "--json"}, {"list", "--sort", "last-active", "--wide"}, {"status", result.Name, "--json"}, {"status", "--all"}, {"status", "--all", "--json"}, {"session", "show", result.Name, "--json"}, {"network", "env", result.Name, "--get", "DEVBOX_HOST"}, {"logs", result.Name}} {
		if out, err := run(args...); err != nil || out == "" {
			t.Fatal(args, out, err)
		}
	}
	for _, args := range [][]string{{"status"}, {"status", result.Name, "--all"}, {"status", result.Name, result.Name}, {"list", "--sort", "wrong"}, {"session", "list", "--sort", "wrong"}, {"session", "list", "--orphaned"}, {"session", "list", "--older-than", "24h"}} {
		if _, err := run(args...); err == nil {
			t.Fatal("invalid list option accepted", args)
		}
	}
	out, err := run("status", "--all", "--profile", "test", "--json")
	var statusViews []app.View
	if err != nil || json.Unmarshal([]byte(out), &statusViews) != nil || len(statusViews) != 1 || statusViews[0].Name != result.Name || statusViews[0].Desired != "NoChange" {
		t.Fatal("bulk status JSON did not include drift", out, err)
	}
	if out, err := run("status", "--all", "--profile", "absent", "--json"); err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatal("bulk status ignored profile filtering", out, err)
	}
	if out, err := run("status", "--all", "--profile", "absent"); err != nil || !strings.Contains(out, "No matching managed containers.") {
		t.Fatal("incorrect empty bulk status", out, err)
	}
	destination := t.TempDir()
	if out, err := run("session", "clone", result.Name, destination, "--dry-run", "--json"); err != nil || !strings.Contains(out, `"dry_run":true`) {
		t.Fatal(out, err)
	}
	if out, err := run("session", "clone", result.Name, destination, "--json"); err != nil || !strings.Contains(out, `"mode":"clone"`) {
		t.Fatal(out, err)
	}
	for _, order := range []string{"name", "last-active"} {
		out, err := run("session", "list", "--sort", order, "--json")
		if err != nil {
			t.Fatal(out, err)
		}
		var views []app.View
		if err := json.Unmarshal([]byte(out), &views); err != nil || len(views) != 2 {
			t.Fatal("session JSON lost entries", out, err)
		}
		if order == "name" && views[0].Name > views[1].Name || order == "last-active" && views[0].LastActivity.Before(views[1].LastActivity) {
			t.Fatal("session JSON ignored sorting", out)
		}
		table, err := run("session", "list", "--sort", order)
		if err != nil || !strings.Contains(table, "LAST ACTIVE") || !strings.Contains(table, "CONTAINER") || strings.Index(table, views[0].Name) > strings.Index(table, views[1].Name) {
			t.Fatal("session text and JSON disagree", table, err)
		}
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
	if out, err := run("session", "list"); err != nil || !strings.Contains(out, result.Name) || !strings.Contains(out, "missing") {
		t.Fatal("session list hid a missing container", out, err)
	}
	if out, err := run("session", "prune", "--orphaned", "--dry-run"); err != nil || !strings.Contains(out, "Would delete") {
		t.Fatal(out, err)
	}
	if _, err = run("session", "delete", result.Name); err != nil {
		t.Fatal(err)
	}
}
func TestSessionListEmptyOutput(t *testing.T) {
	state, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := &app.Engine{Store: state, Docker: docker.Runtime{Runner: &dockertest.Daemon{}}}
	for _, asJSON := range []bool{false, true} {
		profile := ""
		cmd := sessionCommands(func(*cobra.Command) (*app.Engine, error) { return engine, nil }, &profile)
		var out bytes.Buffer
		cmd.SetOut(&out)
		args := []string{"list"}
		if asJSON {
			args = append(args, "--json")
		}
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if asJSON && strings.TrimSpace(out.String()) != "[]" || !asJSON && !strings.Contains(out.String(), "No durable sessions.") {
			t.Fatal("incorrect empty session output", out.String())
		}
	}
}

func TestRemovedFullDockerfileIsNotAnInitChoice(t *testing.T) {
	home := t.TempDir()
	resourceCLI(t, home, "profile", "create", "test")
	if _, err := resourceCLI(t, home, "profile", "init", "test", "--harness", "pi", "--artifact", "Dockerfile.full"); err == nil {
		t.Fatal("removed full override was seeded")
	}
}
