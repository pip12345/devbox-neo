package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/app"
	"devbox/internal/docker/dockertest"
	"devbox/internal/environment"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func TestSourcePickerHintsRetainExplicitHome(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		home := filepath.Join(t.TempDir(), "home with spaces")
		cmd := &cobra.Command{Use: "create"}
		cmd.Flags().String("home", "", "")
		if explicit {
			if err := cmd.Flags().Set("home", home); err != nil {
				t.Fatal(err)
			}
		}
		var out bytes.Buffer
		m := menu{ctx: context.Background(), in: bufio.NewReader(strings.NewReader("0\n")), out: &out, cmd: cmd}
		picker, err := newSourcePicker(m, home, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, selected, err := picker.choose(nil, "Back"); err != nil || selected {
			t.Fatal(selected, err)
		}
		want := "devbox-neo "
		if explicit {
			want += "--home " + shellQuote(home) + " "
		}
		want += "config create base"
		if !strings.Contains(out.String(), want) || strings.Contains(out.String(), "--home") != explicit {
			t.Fatal("menu hint changed installations", out.String())
		}
	}
}

func TestExplicitCreateAndEditDefaultAreSeparateWorkflows(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	factory := func(*cobra.Command) (*app.Engine, error) { return e, nil }
	name := ""
	create := createCommand(factory, &name)
	var out bytes.Buffer
	e.Docker.Runner.(*dockertest.Daemon).Fail = func(args []string) error {
		if len(args) > 0 && args[0] == "build" {
			out.WriteString("Docker output without newline")
		}
		return nil
	}
	create.SetOut(&out)
	create.SetErr(&out)
	create.SetIn(strings.NewReader(""))
	create.SetArgs([]string{q.Workspace, "--name", "Second", "--config", "base"})
	if err := create.ExecuteContext(context.Background()); err != nil {
		t.Fatal(out.String(), err)
	}
	if !strings.Contains(out.String(), "Docker output without newline\nCreated session Second") || strings.Contains(out.String(), "Choose a number") || !strings.Contains(out.String(), "edit "+shellQuote(q.Workspace)+" --name Second --default") {
		t.Fatal(out.String())
	}
	if selected, err := e.Store.ReadDefault(context.Background(), q.Workspace); err != nil || selected != nil {
		t.Fatal("creation implicitly selected a default", selected, err)
	}
	identity, _ := environment.Identify(q.Workspace, "Second")
	r, err := e.Store.Read(context.Background(), identity.Name)
	if err != nil || len(r.Sources) != 1 || r.Sources[0].Path != q.Sources[0].Path {
		t.Fatal(r.Sources, err)
	}
	name = ""
	edit := editCommand(factory, &name)
	edit.SetOut(&out)
	edit.SetErr(&out)
	edit.SetIn(strings.NewReader(""))
	edit.SetArgs([]string{q.Workspace, "--name", "Second", "--default"})
	if err := edit.ExecuteContext(context.Background()); err != nil {
		t.Fatal(out.String(), err)
	}
	if selected, err := e.Locate(context.Background(), q.Workspace, ""); err != nil || selected.ID != r.ID {
		t.Fatal("edit --default did not select the requested session", selected.ID, err)
	}
}

func TestEditDefaultFlagsSetAndClearWithoutSessionEditor(t *testing.T) {
	e, q, fullName := namedCLIFixture(t)
	ctx := context.Background()
	run := func(args ...string) (string, error) {
		name := ""
		cmd := editCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
		var out bytes.Buffer
		cmd.SetIn(strings.NewReader(""))
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		err := cmd.ExecuteContext(ctx)
		return out.String(), err
	}
	if _, err := run(q.Workspace, "--name", "Main", "--default"); err != nil {
		t.Fatal(err)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected == nil || selected.Name != fullName {
		t.Fatal("named session was not selected", selected, err)
	}
	if _, err := run(q.Workspace, "--clear-default"); err != nil {
		t.Fatal(err)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("folder default was not cleared", selected, err)
	}
	if _, err := run(fullName, "--default"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(fullName, "--clear-default"); err != nil {
		t.Fatal("exact session target could not clear its folder default", err)
	}
	if err := os.Remove(filepath.Join(e.Store.Home, "configs", "base", "config.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := run(fullName, "--default"); err != nil {
		t.Fatal("missing config blocked default selection", err)
	}
	if _, err := run(q.Workspace, "--clear-default"); err != nil {
		t.Fatal("missing config blocked clearing the default", err)
	}
	for _, args := range [][]string{
		{q.Workspace, "--default"},
		{q.Workspace, "--default", "--clear-default"},
		{q.Workspace, "--name", "Main", "--clear-default"},
		{q.Workspace, "--name", "Main", "--default", "--show"},
	} {
		if _, err := run(args...); err == nil {
			t.Fatal("accepted ambiguous default flags", args)
		}
	}
}

func TestEditFolderMenuSetsAndClearsDefault(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	q.LocalName = "Second"
	if _, err := e.Create(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	master, slave := testTerminal(t)
	if _, err := master.WriteString("3\n2\n4\n0\n"); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := editCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	var out bytes.Buffer
	cmd.SetIn(slave)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{q.Workspace})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	text := out.String()
	if !strings.Contains(text, "[3]  Set folder default") || !strings.Contains(text, "[4]  Clear folder default") || !strings.Contains(text, "[0]  Exit") || strings.Contains(text, "[0]  Cancel") || !strings.Contains(text, "Current selection: No default") || !strings.Contains(text, ": Second\n") || !strings.Contains(text, "[2]  * Second") || !strings.Contains(text, "[1]    Main") || !strings.Contains(text, "Cleared default session for ") || strings.Contains(text, "default · stopped") || strings.Contains(text, "Make folder default") {
		t.Fatal("default actions were not available in the folder menu", text)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("folder default was not cleared", selected, err)
	}
}

func TestEditFolderExitKeepsSavedDefault(t *testing.T) {
	e, q, fullName := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if _, err := master.WriteString("2\n1\n0\n"); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := editCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	var out bytes.Buffer
	cmd.SetIn(slave)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{q.Workspace})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(out.String(), err)
	}
	if !strings.Contains(out.String(), "[0]  Exit") || strings.Contains(out.String(), "[0]  Cancel") || !strings.HasSuffix(strings.TrimSpace(out.String()), "Default session for "+displayCell(q.Workspace)+": Main") {
		t.Fatal("exiting the folder overview lost its saved-action receipt", out.String())
	}
	selected, err := e.Store.ReadDefault(context.Background(), q.Workspace)
	if err != nil || selected == nil || selected.Name != fullName {
		t.Fatal("exiting the folder overview lost its saved default", selected, err)
	}
}

func TestEditCanClearStaleDefaultWithoutSessions(t *testing.T) {
	e, q, fullName := namedCLIFixture(t)
	ctx := context.Background()
	r, err := e.Store.Read(ctx, fullName)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetDefault(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(e.Store.Home, "sessions", fullName)); err != nil {
		t.Fatal(err)
	}
	master, slave := testTerminal(t)
	if _, err := master.WriteString("1\n"); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := editCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	var out bytes.Buffer
	cmd.SetIn(slave)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{q.Workspace})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	if !strings.Contains(out.String(), "Clear folder default") || !strings.Contains(out.String(), "Cleared default session for ") {
		t.Fatal("missing session hid its saved default", out.String())
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("stale default was not cleared", selected, err)
	}
}

func TestEditFolderCanRecoverFromBrokenSessionSelection(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	q.LocalName = "Broken"
	broken, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	record, err := e.Store.Read(context.Background(), broken.Name)
	if err != nil {
		t.Fatal(err)
	}
	record.Version = 0
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path, err := e.Store.RecordPath(broken.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	master, slave := testTerminal(t)
	if _, err := master.WriteString("1\n2\n0\n0\n"); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := editCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	var out bytes.Buffer
	cmd.SetIn(slave)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{q.Workspace})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	if strings.Count(out.String(), "Select a session to edit") < 2 || !strings.Contains(out.String(), "Error: Invalid session state: unsupported session record version") || !strings.Contains(out.String(), "Manage config sources") || !strings.Contains(out.String(), "[0]  Exit") {
		t.Fatal("invalid session selection closed the folder editor", out.String())
	}
}

func TestInteractiveCreationEditsNameAndSourcesBeforeCreating(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if _, err := master.WriteString("1\nFresh\n2\n1\n1\n"); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := createCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	var out bytes.Buffer
	cmd.SetIn(slave)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{q.Workspace})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	text := out.String()
	for _, forbidden := range []string{"Session name: Main", "Select a harness", "Choose optional files", "Select the default session"} {
		if strings.Contains(text, forbidden) {
			t.Fatal("session creation entered another workflow or suggested a name", text)
		}
	}
	if !strings.Contains(text, "Session name: Not set") || !strings.Contains(text, "[1]  Set session name") || !strings.Contains(text, "Session name: Fresh") || !strings.Contains(text, "\nCreated session Fresh") || !strings.Contains(text, "edit "+shellQuote(q.Workspace)) || strings.Contains(text, "--name Fresh") {
		t.Fatal(text)
	}
	nameIndex, pickerIndex, reviewIndex := strings.Index(text, "Session name (:back cancels): "), strings.Index(text, "Select a config source"), strings.Index(text, "[1]  Create session")
	if nameIndex < 0 || pickerIndex < nameIndex || reviewIndex < pickerIndex || !strings.Contains(text, "[1]  Create session\n\n   [2]  Change session name") {
		t.Fatal("creation overview did not keep pending inputs editable", text)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("interactive creation selected a default", selected, err)
	}
}

func TestInteractiveCreationRedrawsEditableNameInTerminal(t *testing.T) {
	t.Setenv("TERM", "xterm")
	e, q, _ := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 80}); err != nil {
		t.Fatal(err)
	}
	if _, err := master.WriteString("1\nFresh\n2\n1\n1\n"); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := createCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)
	cmd.SetArgs([]string{q.Workspace})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := slave.WriteString("\x00"); err != nil {
		t.Fatal(err)
	}
	text, err := bufio.NewReader(master).ReadString('\x00')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Session name: Not set") || !strings.Contains(text, "Session name: Fresh") || !strings.Contains(text, "\x1b[?1049h") || !strings.Contains(text, "\x1b[?1049l") || strings.Index(text, "\nCreated session Fresh") < strings.LastIndex(text, "\x1b[?1049l") {
		t.Fatal("creation did not show the editable draft and restore the shell before creating", text)
	}
}

func TestInteractiveCreationCanBackOutOfInputsAndCancelOverview(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if _, err := master.WriteString("1\nFresh\n2\n0\n1\n:back\n0\n"); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := createCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	var out bytes.Buffer
	cmd.SetIn(slave)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{q.Workspace})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	if !strings.Contains(out.String(), "Select a config source") || !strings.Contains(out.String(), "[0]  Back") || strings.Count(out.String(), "Session name: Fresh") < 2 || !strings.Contains(out.String(), "Cancelled. No session was created.") {
		t.Fatal(out.String())
	}
	identity, err := environment.Identify(q.Workspace, "Fresh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.Read(ctx, identity.Name); !os.IsNotExist(err) {
		t.Fatal("cancelling the creation overview created a session", err)
	}
}

func TestInteractiveCreationCanChooseSourcesBeforeNameAndChangeName(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if _, err := master.WriteString("2\n1\n1\nbad name\nFirst\n2\n:back\n2\nRenamed\n1\n"); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := createCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	var out bytes.Buffer
	cmd.SetIn(slave)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{q.Workspace})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	text := out.String()
	if !strings.Contains(text, "Error: session name must be") || !strings.Contains(text, "Session name: First") || !strings.Contains(text, "Current name: First") || !strings.Contains(text, "Created session Renamed") || strings.Contains(text, "Created session First") {
		t.Fatal("editing the pending name changed the wrong state", text)
	}
	for _, local := range []string{"First", "Renamed"} {
		identity, err := environment.Identify(q.Workspace, local)
		if err != nil {
			t.Fatal(err)
		}
		record, err := e.Store.Read(ctx, identity.Name)
		if local == "First" && !os.IsNotExist(err) || local == "Renamed" && (err != nil || len(record.Sources) != 1) {
			t.Fatal("creation saved the wrong pending name or sources", local, record, err)
		}
	}
}

func TestInteractiveCreationPrefillsProvidedInputs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		input      string
		wantPicker bool
	}{
		{"name", []string{"--name", "OnlyName"}, "2\n1\n1\n", true},
		{"config", []string{"--config", "base"}, "1\nOnlyConfig\n1\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, q, _ := namedCLIFixture(t)
			master, slave := testTerminal(t)
			if _, err := master.WriteString(tc.input); err != nil {
				t.Fatal(err)
			}
			name := ""
			cmd := createCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
			var out bytes.Buffer
			cmd.SetIn(slave)
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(append([]string{q.Workspace}, tc.args...))
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := cmd.ExecuteContext(ctx); err != nil {
				t.Fatal(out.String(), err)
			}
			if strings.Contains(out.String(), "Select a config source") != tc.wantPicker || !strings.Contains(out.String(), "Created session Only") {
				t.Fatal("provided inputs were not retained in the creation overview", out.String())
			}
		})
	}
}

func TestEditReportsSavedSourcesAfterExitOnlyWhenChanged(t *testing.T) {
	e, _, fullName := namedCLIFixture(t)
	factory := func(*cobra.Command) (*app.Engine, error) { return e, nil }
	run := func(input string) string {
		t.Helper()
		master, slave := testTerminal(t)
		if _, err := master.WriteString(input); err != nil {
			t.Fatal(err)
		}
		name := ""
		cmd := editCommand(factory, &name)
		var out bytes.Buffer
		cmd.SetIn(slave)
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{fullName})
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatal(out.String(), err)
		}
		return out.String()
	}
	changed := run("3\n1\n0\n")
	if !strings.Contains(changed, "Saved config sources.") || !strings.Contains(changed, "Sources saved; container changes may still be pending. Check with:\n  devbox-neo status "+fullName) {
		t.Fatal("source edit lost its saved-but-not-applied receipt", changed)
	}
	if unchanged := run("0\n"); strings.Contains(unchanged, "Sources saved;") {
		t.Fatal("no-op edit claimed to have saved sources", unchanged)
	}
}

func TestEditReceiptListsEachChangedSessionOnce(t *testing.T) {
	var out bytes.Buffer
	m := menu{out: &out, cmd: &cobra.Command{Use: "edit"}}
	if err := writeEditReceipts(m, "", map[string]bool{"devbox-z": true, "devbox-a": true}, ""); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Count(text, "Sources saved;") != 1 || strings.Count(text, "devbox-neo status devbox-a") != 1 || strings.Count(text, "devbox-neo status devbox-z") != 1 || strings.Index(text, "devbox-a") >= strings.Index(text, "devbox-z") {
		t.Fatal("receipt lost or duplicated a changed session", text)
	}
}

func TestSavedSourceMenuPersistsIncompleteEditsWithoutNestedEditors(t *testing.T) {
	e, _, fullName := namedCLIFixture(t)
	r, err := e.Store.Read(context.Background(), fullName)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	m := menu{ctx: context.Background(), in: bufio.NewReader(strings.NewReader("3\n1\n0\n")), out: &out}
	saved, err := sourceChainMenu(m, e, r, "Exit")
	if err != nil || !saved {
		t.Fatal(out.String(), saved, err)
	}
	after, err := e.Store.Read(context.Background(), fullName)
	if err != nil || after.ID != r.ID || len(after.Sources) != 0 || after.Applied != r.Applied {
		t.Fatal("source edit was lost or applied container settings", after, err)
	}
	if !strings.Contains(out.String(), "Saved config sources.") || !strings.Contains(out.String(), "at least one configuration source") || !strings.Contains(out.String(), "[0]  Exit") {
		t.Fatal(out.String())
	}
	for _, forbidden := range []string{"Select a harness", "Choose optional files", "Select the default session"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatal("source menu entered another workflow", out.String())
		}
	}
}
