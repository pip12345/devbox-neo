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
)

func TestEmptySourcePickerOffersSharedCreation(t *testing.T) {
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
		m := testMenuCommand(context.Background(), bufio.NewReader(strings.NewReader("0\n")), &out, cmd)
		picker, err := newSourcePicker(m, home, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, selected, err := picker.choose(nil, "Back"); err != nil || selected {
			t.Fatal(selected, err)
		}
		if !strings.Contains(out.String(), "Create and add config") || strings.Contains(out.String(), "config create base") {
			t.Fatal("empty picker must offer inline creation, not send the user away", out.String())
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
	r, err := e.Store.Find(context.Background(), "", &identity.Binding)
	if err != nil || len(r.Settings.Sources) != 1 || r.Settings.Sources[0].Path != q.Sources[0].Path {
		t.Fatal(r.Settings.Sources, err)
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
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected == nil || selected.ID != fullName {
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
	if _, err := master.WriteString("3\n14\n14\n0\n0\n"); err != nil {
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
	if !strings.Contains(text, "Session · Second") || !strings.Contains(text, "Make folder default") || !strings.Contains(text, "Clear folder default") || !strings.Contains(text, "[0]  Exit") || !strings.Contains(text, "Cleared default session for ") || strings.Contains(text, "Select a session to edit") {
		t.Fatal("edit did not use the browser's session default actions", text)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("folder default was not cleared", selected, err)
	}
}

func TestEditFolderExitKeepsSavedDefault(t *testing.T) {
	e, q, fullName := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if _, err := master.WriteString("2\n14\n0\n0\n"); err != nil {
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
	if err != nil || selected == nil || selected.ID != fullName {
		t.Fatal("exiting the folder overview lost its saved default", selected, err)
	}
}

func TestEditCanClearStaleDefaultWithoutSessions(t *testing.T) {
	e, q, fullName := namedCLIFixture(t)
	ctx := context.Background()
	r, err := e.Store.Find(ctx, fullName, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetDefault(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(e.Store.Home, "sessions", sessionRecord(t, e, fullName).Directory)); err != nil {
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
	record, err := e.Store.Find(context.Background(), broken.SessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	record.Version = 0
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path, err := e.Store.RecordPath(sessionRecord(t, e, broken.SessionID).Directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	master, slave := testTerminal(t)
	if _, err := master.WriteString("2\n13\n0\n3\n13\n0\n0\n0\n"); err != nil {
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
	if !strings.Contains(out.String(), "Error: Invalid session state: unsupported session record version") || !strings.Contains(out.String(), "Manage configs") || !strings.Contains(out.String(), "[0]  Exit") {
		t.Fatal("invalid session selection closed the browser", out.String())
	}
}

func TestInteractiveCreationEditsNameAndSourcesBeforeCreating(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if _, err := master.WriteString("1\nFresh\n2\n1\n8\n0\n"); err != nil {
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
	if !strings.Contains(text, "Session name: Not set") || !strings.Contains(text, "[1]  Set session name") || !strings.Contains(text, "Session name: Fresh") || !strings.Contains(text, "Session · Fresh") {
		t.Fatal(text)
	}
	nameIndex, pickerIndex := strings.Index(text, "Session name (:back cancels): "), strings.Index(text, "Select an existing config")
	if nameIndex < 0 || pickerIndex < nameIndex || !strings.Contains(text, "[1]  Change session name") {
		t.Fatal("creation overview did not keep pending inputs editable", text)
	}
	for _, label := range []string{"Configs, in order:", "[2]  Add existing config", "[3]  Create config", "[4]  Replace config", "[5]  Remove config", "[6]  Change folder", "[8]  Create session"} {
		if !strings.Contains(text, label) {
			t.Fatal("creation menu mixed config and source labels", label, text)
		}
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("interactive creation selected a default", selected, err)
	}
}

func TestInteractiveCreationRedrawsEditableNameInTerminal(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	p := newTerminalProbe(t)
	done := p.workflow(func(ctx context.Context, tty *os.File) error {
		name := ""
		cmd := createCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		cmd.SetErr(tty)
		cmd.SetArgs([]string{q.Workspace})
		return cmd.ExecuteContext(ctx)
	})
	p.wait("Set session name")
	p.send("\r")
	p.send("Fresh\r")
	p.send("\x1b[B\r")
	p.wait("Select an existing config")
	p.send("\r")
	p.send("\x1b[F\r")
	p.wait("Press Enter")
	p.send("\r")
	p.wait("Session · Fresh")
	p.send("q")
	p.finish(done)
	text := p.output()
	if !strings.Contains(text, "\x1b[?1049h") || !strings.Contains(text, "Session · Fresh") {
		t.Fatal("creation did not return to the shared session menu", text)
	}
	r, err := e.Locate(context.Background(), q.Workspace, "Fresh")
	if err != nil || r.Settings.LocalName != "Fresh" {
		t.Fatal("native draft did not create the chosen identity", r, err)
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
	if !strings.Contains(out.String(), "Select an existing config") || !strings.Contains(out.String(), "[0]  Back") || strings.Count(out.String(), "Session name: Fresh") < 2 || !strings.Contains(out.String(), "[0]  Cancel") {
		t.Fatal(out.String())
	}
	identity, err := environment.Identify(q.Workspace, "Fresh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.Find(ctx, "", &identity.Binding); !os.IsNotExist(err) {
		t.Fatal("cancelling the creation overview created a session", err)
	}
}

func TestInteractiveCreationCanChooseSourcesBeforeNameAndChangeName(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if _, err := master.WriteString("2\n1\n1\nbad name\nFirst\n1\n:back\n1\nRenamed\n8\n0\n"); err != nil {
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
	if !strings.Contains(text, "Error: session name must be") || !strings.Contains(text, "Session name: First") || !strings.Contains(text, "Current name: First") || !strings.Contains(text, "Session · Renamed") || strings.Contains(text, "Session · First") {
		t.Fatal("editing the pending name changed the wrong state", text)
	}
	for _, local := range []string{"First", "Renamed"} {
		identity, err := environment.Identify(q.Workspace, local)
		if err != nil {
			t.Fatal(err)
		}
		record, err := e.Store.Find(ctx, "", &identity.Binding)
		if local == "First" && !os.IsNotExist(err) || local == "Renamed" && (err != nil || len(record.Settings.Sources) != 1) {
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
		{"name", []string{"--name", "OnlyName"}, "2\n1\n8\n0\n", true},
		{"config", []string{"--config", "base"}, "1\nOnlyConfig\n8\n0\n", false},
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
			if strings.Contains(out.String(), "Select an existing config") != tc.wantPicker || !strings.Contains(out.String(), "Session · Only") {
				t.Fatal("provided inputs were not retained in the creation overview", out.String())
			}
		})
	}
}

func TestEditReportsSavedSourcesAfterExitOnlyWhenChanged(t *testing.T) {
	e, _, fullName := namedCLIFixture(t)
	if out, err := resourceCLI(t, e.Store.Home, "config", "create", "incomplete", "--json"); err != nil {
		t.Fatal(out, err)
	}
	if out, err := runSourcesCLI(t, e, fullName, "--config", "base", "--config", "incomplete"); err != nil {
		t.Fatal(out, err)
	}
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
	changed := run("4\n1\n0\n")
	if !strings.Contains(changed, "Saved selected configs.") || !strings.Contains(changed, "Selected configs saved; container changes may still be pending.") || !strings.Contains(changed, "dbx status "+fullName) {
		t.Fatal("source edit lost its saved-but-not-applied receipt", changed)
	}
	if unchanged := run("0\n"); strings.Contains(unchanged, "Selected configs saved;") {
		t.Fatal("no-op edit claimed to have saved sources", unchanged)
	}
}

func TestSavedSourceMenuPersistsIncompleteEditsWithoutNestedEditors(t *testing.T) {
	e, _, fullName := namedCLIFixture(t)
	if out, err := resourceCLI(t, e.Store.Home, "config", "create", "incomplete", "--json"); err != nil {
		t.Fatal(out, err)
	}
	if out, err := runSourcesCLI(t, e, fullName, "--config", "base", "--config", "incomplete"); err != nil {
		t.Fatal(out, err)
	}
	r, err := e.Store.Find(context.Background(), fullName, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	m := testMenu(context.Background(), bufio.NewReader(strings.NewReader("4\n1\n0\n")), &out)
	saved, err := sourceChainMenu(m, e, r, "Exit")
	if err != nil || !saved {
		t.Fatal(out.String(), saved, err)
	}
	after, err := e.Store.Find(context.Background(), fullName, nil)
	if err != nil || after.ID != r.ID || len(after.Settings.Sources) != 1 || after.Settings.Sources[0].Label != "incomplete" || after.Applied.Fingerprints != r.Applied.Fingerprints {
		t.Fatal("source edit was lost or applied container settings", after, err)
	}
	for _, label := range []string{"Manage configs", "Configs, in order:", "Add existing config", "Replace config", "Remove config", "Select a config", "Saved selected configs.", "Configuration error:", "[0]  Exit"} {
		if !strings.Contains(out.String(), label) {
			t.Fatal("session editor mixed config and source labels", label, out.String())
		}
	}
	for _, forbidden := range []string{"Select a harness", "Choose optional files", "Select the default session"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatal("source menu entered another workflow", out.String())
		}
	}
}
