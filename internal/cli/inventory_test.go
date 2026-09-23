package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func inventoryCLI(t *testing.T) (*app.Engine, *dockertest.Daemon, string, func() *cobra.Command) {
	t.Helper()
	ctx := context.Background()
	state, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resources := resource.Service{Home: state.Home}
	owner, _ := resources.ConfigDirectory("test", t.TempDir(), t.TempDir())
	if _, err := resources.CreateConfig(ctx, owner, resource.SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := resources.EditConfig(ctx, owner, resource.SetupOptions{Harness: harnessSetting("pi")}); err != nil {
		t.Fatal(err)
	}
	daemon := &dockertest.Daemon{}
	engine := &app.Engine{Store: state, Docker: docker.Runtime{Runner: daemon}, UID: 1000, GID: 1000}
	created, err := engine.Create(ctx, app.Request{Workspace: t.TempDir(), LocalName: "test", Sources: testConfigSources(engine.Store.Home, "test")})
	if err != nil {
		t.Fatal(err)
	}
	root := func() *cobra.Command {
		cmd := &cobra.Command{Use: "devbox-neo", SilenceErrors: true, SilenceUsage: true}
		profile := ""
		cmd.PersistentFlags().StringVar(&profile, "profile", "", "Profile")
		cmd.AddCommand(sessionCommands(func(*cobra.Command) (*app.Engine, error) { return engine, nil }, &profile)...)
		return cmd
	}
	return engine, daemon, created.Name, root
}

func TestFolderListStartsWithItsHeading(t *testing.T) {
	engine, _, name, root := inventoryCLI(t)
	record, err := engine.Store.Read(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	cmd := root()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", record.Identity.Workspace, "--sort", "folder"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(out.String(), "\n") || strings.Contains(out.String(), "\n\n") || !strings.Contains(strings.ReplaceAll(out.String(), "\n", ""), record.Identity.Workspace) || !strings.Contains(out.String(), "\nNAME") || strings.Contains(out.String(), "FOLDER") {
		t.Fatal("folder list changed its compact layout", out.String())
	}
}

func TestGlobalListShowsFolderPerRowAndSortsByFolder(t *testing.T) {
	engine, _, firstName, root := inventoryCLI(t)
	first, err := engine.Store.Read(context.Background(), firstName)
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	second, err := engine.Create(context.Background(), app.Request{Workspace: other, LocalName: "test", Sources: testConfigSources(engine.Store.Home, "test")})
	if err != nil {
		t.Fatal(err)
	}
	cmd := root()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	folders := []string{first.Identity.Workspace, other}
	sort.Strings(folders)
	if !strings.HasPrefix(text, "FOLDER") || strings.Index(text, folders[0]) < 0 || strings.Index(text, folders[1]) < 0 || strings.Index(text, folders[0]) >= strings.Index(text, folders[1]) || !strings.Contains(text, firstName) || !strings.Contains(text, second.Name) || strings.Contains(text, "\n\n") {
		t.Fatal("global list did not render and sort folder rows", text)
	}
	cmd = root()
	var explicit bytes.Buffer
	cmd.SetOut(&explicit)
	cmd.SetArgs([]string{"list", "--sort", "folder"})
	if err := cmd.Execute(); err != nil || explicit.String() != text {
		t.Fatal("default list order differs from --sort folder", err, explicit.String(), text)
	}
}

func TestListAndStatusWarnWithoutInventingSessionRows(t *testing.T) {
	engine, _, name, root := inventoryCLI(t)
	p, _ := engine.Store.RecordPath(name)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"list"}, {"status", "--all"}} {
		cmd := root()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Warning: managed containers with no session record:") || strings.Count(out.String(), name) != 1 || !strings.Contains(out.String(), "(stopped)") || strings.Contains(out.String(), "\n\n") {
			t.Fatal("missing warning or invented row", out.String())
		}
		cmd = root()
		out.Reset()
		cmd.SetOut(&out)
		cmd.SetArgs(append(args, "--json"))
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var report app.InventoryReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil || len(report.Sessions) != 0 || len(report.UnmatchedContainers) != 1 || report.UnmatchedContainers[0].Name != name {
			t.Fatal("JSON lost structured warning", out.String(), err)
		}
	}
}

func TestDeleteCLIFlagsAndTerminalPrompts(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		flags                  []string
		input                  string
		wantError, sessionGone bool
		prompts                int
	}{
		{name: "requires confirmation", wantError: true},
		{name: "force is not confirmation", flags: []string{"--force"}, wantError: true},
		{name: "container scope keeps state", flags: []string{"--container"}},
		{name: "session scope deletes state", flags: []string{"--session"}, sessionGone: true},
		{name: "scopes conflict", flags: []string{"--session", "--container"}, wantError: true},
		{name: "old yes removed", flags: []string{"--yes"}, wantError: true},
		{name: "old include removed", flags: []string{"--include-session"}, wantError: true},
		{name: "preview needs scope", flags: []string{"--dry-run"}, wantError: true},
		{name: "zero age rejected", flags: []string{"--container", "--older-than", "0"}, wantError: true},
		{name: "negative age rejected", flags: []string{"--container", "--older-than", "-1h"}, wantError: true},
		{name: "target and filters rejected", flags: []string{"--container", "--stopped"}, wantError: true},
		{name: "json does not prompt", flags: []string{"--json"}, wantError: true},
		{name: "interactive keeps state", input: "y\nn\n", prompts: 2},
		{name: "interactive deletes state", input: "y\ny\n", prompts: 2, sessionGone: true},
		{name: "explicit scope never prompts", flags: []string{"--session"}, input: "n\n", sessionGone: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, daemon, name, root := inventoryCLI(t)
			cmd := root()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetIn(strings.NewReader(""))
			if tc.input != "" {
				master, slave := testTerminal(t)
				cmd.SetIn(slave)
				if _, err := master.WriteString(tc.input); err != nil {
					t.Fatal(err)
				}
			}
			cmd.SetArgs(append([]string{"delete", name}, tc.flags...))
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := cmd.ExecuteContext(ctx)
			if (err != nil) != tc.wantError {
				t.Fatal(out.String(), err)
			}
			p, _ := engine.Store.RecordPath(name)
			_, statErr := os.Stat(p)
			if os.IsNotExist(statErr) != tc.sessionGone {
				t.Fatal("wrong saved-state outcome", out.String(), statErr)
			}
			if tc.wantError {
				if _, exists := daemon.Snapshot(name); !exists {
					t.Fatal("invalid flags deleted container")
				}
			}
			if strings.Count(out.String(), "Continue? [y/N]") != tc.prompts {
				t.Fatal(out.String())
			}
		})
	}
}

func TestTopLevelCommandsHaveNoSessionCompatibilityGroup(t *testing.T) {
	root := New()
	for _, name := range []string{"list", "status", "copy", "delete"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd.Name() != name {
			t.Fatal(name, err)
		}
	}
	for _, name := range []string{"session", "show", "reset", "prune", "clone", "relocate"} {
		// Group validation now owns unknown-command errors; Find can return the
		// root for validation without making the removed name a real command.
		cmd := New()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs([]string{name})
		if Execute(context.Background(), cmd) == 0 || !strings.Contains(output.String(), "unknown command") {
			t.Fatal("obsolete command accepted", name, output.String())
		}
	}
}
