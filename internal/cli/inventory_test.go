package cli

import (
	"bytes"
	"context"
	"devbox/internal/cliui"
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
		cmd := &cobra.Command{Use: "dbx", SilenceErrors: true, SilenceUsage: true}
		profile := ""
		cmd.PersistentFlags().StringVar(&profile, "profile", "", "Profile")
		cmd.AddCommand(sessionCommands(func(*cobra.Command) (*app.Engine, error) { return engine, nil }, &profile)...)
		return cmd
	}
	return engine, daemon, created.SessionID, root
}

func TestFolderListStartsWithItsHeading(t *testing.T) {
	engine, _, name, root := inventoryCLI(t)
	record, err := engine.Store.Find(context.Background(), name, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd := root()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", record.Settings.Workspace, "--sort", "folder"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(out.String(), "\n") || strings.Contains(out.String(), "\n\n") || !strings.Contains(strings.ReplaceAll(out.String(), "\n", ""), record.Settings.Workspace) || !strings.Contains(out.String(), "\nNAME") || strings.Contains(out.String(), "FOLDER") {
		t.Fatal("folder list changed its compact layout", out.String())
	}
}

func TestGlobalListShowsFolderPerRowAndSortsByFolder(t *testing.T) {
	engine, _, firstName, root := inventoryCLI(t)
	first, err := engine.Store.Find(context.Background(), firstName, nil)
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
	folders := []string{first.Settings.Workspace, other}
	sort.Strings(folders)
	if !strings.HasPrefix(text, "FOLDER") || strings.Index(text, folders[0]) < 0 || strings.Index(text, folders[1]) < 0 || strings.Index(text, folders[0]) >= strings.Index(text, folders[1]) || strings.Contains(text, firstName) || strings.Contains(text, second.SessionID) || strings.Contains(text, "\n\n") {
		t.Fatal("global list did not render and sort folder rows", text)
	}
	rows := strings.Split(strings.TrimSpace(text), "\n")[1:]
	if len(rows) != 2 {
		t.Fatal("missing session rows", text)
	}
	for _, row := range rows {
		fields := strings.Fields(row)
		if len(fields) < 2 || fields[1] != "test" {
			t.Fatal("global list lost local names", text)
		}
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
	containerName := sessionRecord(t, engine, name).Applied.Creation.Name
	p, _ := engine.Store.RecordPath(sessionRecord(t, engine, name).Directory)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"list"}, {"status"}} {
		cmd := root()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Warning: managed containers with no session record:") || strings.Count(out.String(), containerName) != 1 || !strings.Contains(out.String(), "(stopped)") || strings.Contains(out.String(), "\n\n") {
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
		if err := json.Unmarshal(out.Bytes(), &report); err != nil || len(report.Sessions) != 1 || len(report.UnmatchedContainers) != 1 || report.UnmatchedContainers[0].ContainerName != containerName {
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
		{name: "interactive keeps state", input: "1\n2\n4\ny\nn\n", prompts: 2},
		{name: "interactive deletes state", input: "1\n2\n4\ny\ny\n", prompts: 2, sessionGone: true},
		{name: "explicit scope never prompts", flags: []string{"--session"}, input: "n\n", sessionGone: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, _, name, root := inventoryCLI(t)
			path, _ := engine.Store.RecordPath(sessionRecord(t, engine, name).Directory)
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
			_, statErr := os.Stat(path)
			if os.IsNotExist(statErr) != tc.sessionGone {
				t.Fatal("wrong saved-state outcome", out.String(), statErr)
			}
			if tc.wantError {
				if _, exists := sessionSnapshot(t, engine, name); !exists {
					t.Fatal("invalid flags deleted container")
				}
			}
			if strings.Count(out.String(), "[y/N]") != tc.prompts || strings.Contains(out.String(), "Continue?") {
				t.Fatal(out.String())
			}
			if tc.prompts > 0 && !strings.Contains(out.String(), "Remove container? [y/N]") {
				t.Fatal("container confirmation is unclear", out.String())
			}
			if tc.prompts == 2 && !strings.Contains(out.String(), "Container removed. Delete saved data and history? [y/N]") {
				t.Fatal("saved-data confirmation is unclear", out.String())
			}
		})
	}
}

func TestDeleteConfirmationNamesBulkScopeWithoutClaimingDefault(t *testing.T) {
	for _, prompt := range []app.DeletePrompt{
		{Containers: []string{"dbx-a.one", "dbx-b.two"}},
		{Sessions: []string{"dbx-a.one", "dbx-b.two"}},
	} {
		var out bytes.Buffer
		confirmation := deletionConfirmation{ui: cliui.New(context.Background(), strings.NewReader("n\n"), &out)}
		ok, err := confirmation.confirm(prompt)
		if err != nil || ok || strings.Contains(out.String(), "(default in ") || !strings.Contains(out.String(), "[y/N]") {
			t.Fatal("bulk confirmation claimed a folder default or lost its scope", out.String(), err)
		}
		if len(prompt.Containers) > 0 && (!strings.Contains(out.String(), "Remove containers?") || !strings.Contains(out.String(), "dbx-a.one") || !strings.Contains(out.String(), "dbx-b.two")) || len(prompt.Sessions) > 0 && (!strings.Contains(out.String(), "Delete saved data and history?") || !strings.Contains(out.String(), "dbx-a.one") || !strings.Contains(out.String(), "dbx-b.two")) {
			t.Fatal("bulk confirmation used a singular or ambiguous question", out.String())
		}
	}
}

func TestDeleteFolderExplainsItsSelectedDefaultAndPhases(t *testing.T) {
	engine, _, name, root := inventoryCLI(t)
	ctx := context.Background()
	selected, err := engine.Store.Find(ctx, name, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := engine.Create(ctx, app.Request{Workspace: selected.Settings.Workspace, LocalName: "other", Sources: testConfigSources(engine.Store.Home, "test")})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetDefault(ctx, selected); err != nil {
		t.Fatal(err)
	}
	run := func(input string) string {
		t.Helper()
		master, slave := testTerminal(t)
		if _, err := master.WriteString(input); err != nil {
			t.Fatal(err)
		}
		cmd := root()
		var out bytes.Buffer
		cmd.SetIn(slave)
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"delete", selected.Settings.Workspace})
		if err := cmd.ExecuteContext(ctx); err != nil {
			t.Fatal(out.String(), err)
		}
		return out.String()
	}
	cancelled := run("1\n2\n4\nn\n0\n")
	label := "Delete · " + selected.Settings.LocalName
	if !strings.Contains(cancelled, label) || !strings.Contains(cancelled, "Folder: "+selected.Settings.Workspace) || !strings.Contains(cancelled, "Container: "+selected.Applied.Creation.Name) || !strings.Contains(cancelled, "Remove container? [y/N]") || !strings.Contains(cancelled, "Cancelled.") || strings.Contains(cancelled, "Delete saved data and history?") {
		t.Fatal("folder default selection or first phase was unclear", cancelled)
	}
	if _, exists := sessionSnapshot(t, engine, name); !exists {
		t.Fatal("declining container deletion removed the selected default")
	}
	kept := run("1\n2\n4\ny\nn\n")
	if !strings.Contains(kept, label) || !strings.Contains(kept, "Container removed. Delete saved data and history? [y/N]") || strings.Contains(kept, "Saved session data (including") || !strings.Contains(kept, "Session state and image retained") {
		t.Fatal("saved-data decision did not explain the partial outcome", kept)
	}
	if _, exists := sessionSnapshot(t, engine, name); exists {
		t.Fatal("the chosen container was not removed")
	}
	if _, exists := sessionSnapshot(t, engine, other.SessionID); !exists {
		t.Fatal("deleting the folder default removed another session")
	}
	if _, err := engine.Store.Find(ctx, name, nil); err != nil {
		t.Fatal("declining saved-data deletion removed the session", err)
	}
	missing := run("1\n2\n4\nn\n0\n")
	if !strings.Contains(missing, label) || strings.Contains(missing, "Remove container?") || !strings.Contains(missing, "No container.") || !strings.Contains(missing, "Delete saved data and history? [y/N]") {
		t.Fatal("missing container did not go directly to saved-data decision", missing)
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
