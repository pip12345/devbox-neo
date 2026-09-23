package cli

import (
	"bufio"
	"bytes"
	"context"
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
	if !strings.Contains(text, "[3]  Set folder default") || !strings.Contains(text, "[4]  Clear folder default") || !strings.Contains(text, "Current selection: No default") || !strings.Contains(text, ": Second\n") || !strings.Contains(text, "Cleared default session for ") || strings.Contains(text, "Make folder default") {
		t.Fatal("default actions were not available in the folder menu", text)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("folder default was not cleared", selected, err)
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

func TestInteractiveCreationStartsWithBlankNameAndOnlySelectsConfigs(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if _, err := master.WriteString("Fresh\n1\n1\n"); err != nil {
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
	for _, forbidden := range []string{"Session name (:back cancels): Main", "Select a harness", "Choose optional files", "Select the default session"} {
		if strings.Contains(text, forbidden) {
			t.Fatal("session creation entered another workflow or suggested a name", text)
		}
	}
	if !strings.HasPrefix(text, "Session name (:back cancels): ") || !strings.Contains(text, "\nCreated session Fresh") || !strings.Contains(text, "edit "+shellQuote(q.Workspace)) || strings.Contains(text, "--name Fresh") {
		t.Fatal(text)
	}
	pickerIndex, reviewIndex := strings.Index(text, "Select a config source"), strings.Index(text, "Create session · Fresh")
	if pickerIndex < 0 || reviewIndex < 0 || pickerIndex > reviewIndex || !strings.Contains(text, "[1]  Create session\n\n   [2]  Add source") {
		t.Fatal("source picker did not open first or create action was not separated", text)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("interactive creation selected a default", selected, err)
	}
}

func TestInteractiveCreationCanCancelAtInitialSourcePicker(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if _, err := master.WriteString("Fresh\n0\n"); err != nil {
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
	if !strings.Contains(out.String(), "Select a config source") || !strings.Contains(out.String(), "[0]  Cancel") || !strings.Contains(out.String(), "Cancelled. No session was created.") || strings.Contains(out.String(), "Create session · Fresh") {
		t.Fatal(out.String())
	}
	identity, err := environment.Identify(q.Workspace, "Fresh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.Read(ctx, identity.Name); !os.IsNotExist(err) {
		t.Fatal("cancelling the picker created a session", err)
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
	if err := sourceChainMenu(m, e, r, "Done"); err != nil {
		t.Fatal(out.String(), err)
	}
	after, err := e.Store.Read(context.Background(), fullName)
	if err != nil || after.ID != r.ID || len(after.Sources) != 0 || after.Applied != r.Applied {
		t.Fatal("source edit was lost or applied container settings", after, err)
	}
	if !strings.Contains(out.String(), "Saved config sources.") || !strings.Contains(out.String(), "at least one configuration source") || !strings.Contains(out.String(), "[0]  Done") {
		t.Fatal(out.String())
	}
	for _, forbidden := range []string{"Select a harness", "Choose optional files", "Select the default session"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatal("source menu entered another workflow", out.String())
		}
	}
}
