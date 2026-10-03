package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/environment"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func TestStatusTableSeparatesLiveStateFromChanges(t *testing.T) {
	views := []app.View{
		{Target: "clean", Exists: true, Running: true, Desired: environment.NoChange},
		{Target: "runtime", Exists: true, Desired: environment.RuntimeSync},
		{Target: "container", Exists: true, Running: true, Desired: environment.Recreate},
		{Target: "image", Exists: true, Desired: environment.RebuildAndRecreate},
		{Target: "invalid", Exists: true, Running: true, ConfigError: "invalid config\nnext line"},
		{Target: "broken", Exists: true, Error: "corrupt record"},
		{Target: "pending", Exists: true, Pending: &store.Reservation{Mode: "relocate", Phase: "prepare"}},
		{Target: "unknown", Exists: true},
	}
	var out bytes.Buffer
	if err := printStatusList(&cobra.Command{}, &out, views, ""); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"NAME", "CONTAINER", "CHANGE", "No changes", "Runtime changes", "Recreate needed", "Rebuild + recreate needed", "corrupt record", `invalid config\nnext line`, "dbx recreate container", "dbx recreate image", "pending copy --move"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Count(text, "Cannot check") != 4 || strings.Count(text, "dbx recreate") != 2 || strings.Contains(text, "--image") || strings.Contains(text, "\x1b") {
		t.Fatal("incorrect unknown states, actions or escaping", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "invalid ") && (!strings.Contains(line, "running") || strings.Contains(line, "running!")) {
			t.Fatal("desired error changed live state", line)
		}
	}
}

func TestSingleStatusGivesShortManagedFileAndRecreateGuidance(t *testing.T) {
	e, _, name := namedCLIFixture(t)
	configPath := filepath.Join(e.Store.Home, "configs", "base")
	if err := os.MkdirAll(filepath.Join(configPath, "pi"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configPath, "pi", "custom.md"), []byte("updated"), 0600); err != nil {
		t.Fatal(err)
	}
	show := func() string {
		t.Helper()
		local := ""
		cmd := statusCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &local)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{name})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	out := show()
	if !strings.Contains(out, "Changes: Runtime changes") || !strings.Contains(out, "Changes apply on container restart.") || strings.Contains(out, "dbx stop") {
		t.Fatal("managed file guidance was not short and accurate", out)
	}
	if err := os.WriteFile(filepath.Join(configPath, "config.json"), []byte(`{"version":1,"harness":"pi","network":"host"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out = show()
	if !strings.Contains(out, "Changes: Recreate needed") || !strings.Contains(out, "To apply changes:\n  dbx recreate "+name) || strings.Contains(out, "Changes apply on container restart.") {
		t.Fatal("container changes need recreation, not a restart hint", out)
	}
}

func TestStatusDoesNotSuggestRestartForLaunchArguments(t *testing.T) {
	e, _, name := namedCLIFixture(t)
	configPath := filepath.Join(e.Store.Home, "configs", "base", "config.json")
	if err := os.WriteFile(configPath, []byte(`{"version":1,"harness":"pi","harness_args":["--help"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	local := ""
	cmd := statusCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &local)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{name})
	if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "Changes: Runtime changes") || strings.Contains(out.String(), "Changes apply on container restart.") {
		t.Fatal("launch-only change was labeled as needing container restart", out.String(), err)
	}
}

func TestSingleStatusJSONReportsDefaultAndDefaultErrors(t *testing.T) {
	e, q, name := namedCLIFixture(t)
	ctx := context.Background()
	r, err := e.Store.Find(ctx, name, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetDefault(ctx, r); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		local := ""
		cmd := statusCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &local)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(ctx); err != nil {
			t.Fatal(out.String(), err)
		}
		return out.String()
	}
	var result struct {
		Default      bool   `json:"default"`
		DefaultError string `json:"default_error"`
	}
	if err := json.Unmarshal([]byte(run(q.Workspace, "--json")), &result); err != nil || !result.Default || result.DefaultError != "" {
		t.Fatal(result, err)
	}
	key, err := store.WorkspaceKey(q.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.Store.Home, "state/workspaces", key+".json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(run(name, "--json")), &result); err != nil || result.Default || result.DefaultError == "" {
		t.Fatal("JSON lost the default-state diagnostic", result, err)
	}
	if text := run(name); !strings.Contains(text, "Default selection unavailable:") || !strings.Contains(text, "Session: "+r.ID) {
		t.Fatal("human status lost details or default-state diagnostic", text)
	}
}

func TestStatusExplainsDriftAfterContainerRemoval(t *testing.T) {
	for _, tc := range []struct {
		name, path, content, classification, reason string
	}{
		{"harness", "config.json", `{"harness":"claude"}`, "Rebuild + recreate needed", "[image] harness: pi -> claude"},
		{"setup", "setup.sh", "echo changed-setup", "Recreate needed", "[container] setup.sh"},
		{"env", "config.json", `{"harness":"pi","env":["TOKEN=private-test-value"]}`, "Recreate needed", "[container] environment variable TOKEN added"},
		{"build context", "docker/Dockerfile", "FROM ${DEVBOX_BASE}\n", "Rebuild + recreate needed", "[image] build context"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, id := namedCLIFixture(t)
			ctx := context.Background()
			if _, err := e.DeleteContainers(ctx, app.Selection{Targets: []string{id}}, false); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(e.Store.Home, "configs/base", tc.path)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{id}, {}} {
				local := ""
				cmd := statusCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &local)
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetArgs(args)
				if err := cmd.ExecuteContext(ctx); err != nil {
					t.Fatal(err)
				}
				text := out.String()
				for _, want := range []string{tc.classification, tc.reason, "dbx recreate " + id} {
					if !strings.Contains(text, want) {
						t.Fatal("lost drift reason or action", want, text)
					}
				}
				if strings.Contains(text, "private-test-value") || strings.Contains(text, "echo changed-setup") {
					t.Fatal("status exposed input contents", text)
				}
			}
		})
	}
}

func TestStatusRejectsAmbiguousSelectionBeforeInitialization(t *testing.T) {
	for _, args := range [][]string{{"status", "--name", "work"}, {"status", "target", "--all"}, {"status", "one", "two"}} {
		root := &cobra.Command{Use: "dbx", SilenceErrors: true, SilenceUsage: true}
		profile := ""
		root.AddCommand(sessionCommands(func(*cobra.Command) (*app.Engine, error) {
			t.Fatal("invalid status selection initialized the home")
			return nil, nil
		}, &profile)...)
		root.SetArgs(args)
		if err := root.Execute(); err == nil {
			t.Fatal("invalid selection accepted", args)
		}
	}
}
