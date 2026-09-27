package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	owner, _ := resources.ConfigDirectory("test", t.TempDir(), t.TempDir())
	resources.CreateConfig(ctx, owner, resource.SetupOptions{})
	resources.EditConfig(ctx, owner, resource.SetupOptions{Harness: harnessSetting("pi")})
	daemon := &dockertest.Daemon{}
	engine := &app.Engine{Store: state, Docker: docker.Runtime{Runner: daemon}, UID: 1000, GID: 1000}
	result, err := engine.Create(ctx, app.Request{Workspace: t.TempDir(), LocalName: "test", Sources: testConfigSources(engine.Store.Home, "test")})
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		root := &cobra.Command{Use: "devbox-neo", SilenceUsage: true, SilenceErrors: true}
		profile := ""
		factory := func(cmd *cobra.Command) (*app.Engine, error) {
			engine.Streams.Out = cmd.OutOrStdout()
			engine.Streams.Err = cmd.ErrOrStderr()
			engine.OnDiagnostic = diagnosticRenderer(cmd.ErrOrStderr())
			return engine, nil
		}
		root.AddCommand(containerCommands(factory, &profile)...)
		root.AddCommand(sessionCommands(factory, &profile)...)
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		err := root.Execute()
		return out.String(), err
	}
	for _, args := range [][]string{{"list", "--json"}, {"list", "--sort", "last-active", "--wide"}, {"status", result.SessionID, "--json"}, {"status"}, {"status", "--json"}, {"status", result.SessionID}, {"network", "env", result.SessionID, "--get", "DEVBOX_HOST"}, {"logs", result.SessionID}} {
		if out, err := run(args...); err != nil || out == "" {
			t.Fatal(args, out, err)
		}
	}
	for _, args := range [][]string{{"status", "--all"}, {"status", result.SessionID, "--all"}, {"status", result.SessionID, result.SessionID}, {"list", "--sort", "wrong"}, {"list", "--orphaned"}, {"list", "--older-than", "24h"}, {"session", "list"}} {
		if _, err := run(args...); err == nil {
			t.Fatal("invalid list option accepted", args)
		}
	}
	out, err := run("status", "--json")
	var statusViews app.InventoryReport
	if err != nil || json.Unmarshal([]byte(out), &statusViews) != nil || len(statusViews.Sessions) != 1 || statusViews.Sessions[0].SessionID != result.SessionID || statusViews.Sessions[0].Desired != "NoChange" {
		t.Fatal("bulk status JSON did not include drift", out, err)
	}
	if out, err := run("status", "--profile", "test"); err == nil {
		t.Fatal("retained removed profile selection", out)
	}
	if out, err := run("status", "--name", "test"); err == nil {
		t.Fatal("accepted a folder-local name without a folder target", out)
	}
	if err := os.WriteFile(filepath.Join(owner.Root, "config.json"), []byte(`{"version":1,"harness":"pi","network":"host","env":["TOKEN=private-status-value"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"status", result.SessionID}, {"status"}} {
		out, err := run(args...)
		if err != nil || !strings.Contains(out, "[container] network: default -> host") || !strings.Contains(out, "environment variable TOKEN added") || strings.Contains(out, "private-status-value") {
			t.Fatal("status text lost reasons or leaked env", out, err)
		}
	}
	out, err = run("status", result.SessionID, "--json")
	var single app.StatusDetails
	if err != nil || json.Unmarshal([]byte(out), &single) != nil || len(single.PendingInputChanges) != 2 || strings.Contains(out, "private-status-value") || !strings.Contains(out, `"pending_input_changes":`) || strings.Contains(out, `"reasons":`) {
		t.Fatal("single status JSON lost reasons or leaked env", out, err)
	}
	out, err = run("status", "--json")
	if err != nil || json.Unmarshal([]byte(out), &statusViews) != nil || len(statusViews.Sessions) != 1 || !reflect.DeepEqual(single.PendingInputChanges, statusViews.Sessions[0].PendingInputChanges) {
		t.Fatal("bulk and single JSON disagree", out, err)
	}
	if single.Record == nil || single.Record.ID != single.SessionID || single.Record.Applied.ImageID == "" || single.Active == nil {
		t.Fatal("single status JSON lost session details", single)
	}
	if strings.Contains(out, `"record":`) || strings.Contains(out, `"active":`) {
		t.Fatal("bulk status gained single-target details", out)
	}
	if err := os.WriteFile(filepath.Join(owner.Root, "config.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = run("status", result.SessionID)
	for _, want := range []string{"Session: " + single.SessionID, "Harness: pi", "Image: " + single.Record.Applied.ImageID, "Active commands: 0", "Changes: Cannot check", "Desired configuration error:"} {
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("status lost %q with invalid config: %s (%v)", want, out, err)
		}
	}
	out, err = run("status", result.SessionID, "--json")
	if err != nil || json.Unmarshal([]byte(out), &single) != nil || single.Record == nil || single.ConfigError == "" {
		t.Fatal("JSON config error hid saved details", out, err)
	}
	if err := os.WriteFile(filepath.Join(owner.Root, "config.json"), []byte(`{"version":1,"harness":"pi","network":"host","env":["TOKEN=private-status-value"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if out, err := run("copy", result.SessionID, destination, "--dry-run", "--json"); err != nil || !strings.Contains(out, `"dry_run":true`) {
		t.Fatal(out, err)
	}
	if out, err := run("copy", result.SessionID, destination, "--json"); err != nil || !strings.Contains(out, `"mode":"clone"`) {
		t.Fatal(out, err)
	}
	for _, order := range []string{"name", "last-active"} {
		out, err := run("list", "--sort", order, "--json")
		if err != nil {
			t.Fatal(out, err)
		}
		var report app.InventoryReport
		if err := json.Unmarshal([]byte(out), &report); err != nil || len(report.Sessions) != 2 {
			t.Fatal("session JSON lost entries", out, err)
		}
		views := report.Sessions
		if order == "name" && views[0].LocalName > views[1].LocalName || order == "last-active" && views[0].LastActivity.Before(views[1].LastActivity) {
			t.Fatal("session JSON ignored sorting", out)
		}
		table, err := run("list", "--sort", order)
		if err != nil || !strings.Contains(table, "LAST ACTIVE") || !strings.Contains(table, "CONTAINER") {
			t.Fatal("session table lost columns", table, err)
		}
		rows := strings.Split(strings.TrimSpace(table), "\n")[1:]
		if len(rows) != len(views) {
			t.Fatal("session text and JSON counts disagree", table)
		}
		for i, view := range views {
			fields := strings.Fields(rows[i])
			if len(fields) < 2 || fields[0] != view.Workspace || fields[1] != view.LocalName || strings.Contains(table, view.SessionID) {
				t.Fatal("session text and JSON identities/order disagree", table, views)
			}
		}
	}
	if _, err := run("copy", result.SessionID, "--move", "--from", "test"); err == nil {
		t.Fatal("incomplete slot flags accepted")
	}
	if _, err = run("delete", result.SessionID); err == nil {
		t.Fatal("non-interactive deletion bypassed confirmation")
	}
	if out, err := run("delete", result.SessionID, "--container"); err != nil || !strings.Contains(out, "retained") {
		t.Fatal(out, err)
	}
	if out, err := run("list", "--wide"); err != nil || !strings.Contains(out, result.SessionID) || !strings.Contains(out, "missing") {
		t.Fatal("session list hid a missing container", out, err)
	}
	if out, err := run("delete", "--session", "--orphaned", "--dry-run"); err != nil || !strings.Contains(out, "Would delete") {
		t.Fatal(out, err)
	}
	if _, err = run("delete", result.SessionID, "--session"); err != nil {
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
		cmd := &cobra.Command{Use: "devbox-neo"}
		cmd.AddCommand(sessionCommands(func(*cobra.Command) (*app.Engine, error) { return engine, nil }, &profile)...)
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
		if asJSON && strings.TrimSpace(out.String()) != `{"sessions":[],"unmatched_containers":[]}` || !asJSON && strings.TrimSpace(out.String()) != "No saved sessions. Use create <folder> to create one." {
			t.Fatal("incorrect empty session output", out.String())
		}
	}
}

func TestRemovedFullDockerfileIsNotAnInitChoice(t *testing.T) {
	home := t.TempDir()
	resourceCLI(t, home, "config", "create", "test")
	if _, err := resourceCLI(t, home, "config", "edit", "test", "--harness", "pi", "--artifact", "Dockerfile.full"); err == nil {
		t.Fatal("removed full override was seeded")
	}
}
