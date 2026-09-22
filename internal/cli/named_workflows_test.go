package cli

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"devbox/internal/app"
	"devbox/internal/environment"
	"github.com/spf13/cobra"
)

func TestExplicitCreateAndSetAreSeparateWorkflows(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	factory := func(*cobra.Command) (*app.Engine, error) { return e, nil }
	name := ""
	create := createCommand(factory, &name)
	var out bytes.Buffer
	create.SetOut(&out)
	create.SetErr(&out)
	create.SetIn(strings.NewReader(""))
	create.SetArgs([]string{q.Workspace, "--name", "Second", "--config", "base"})
	if err := create.ExecuteContext(context.Background()); err != nil {
		t.Fatal(out.String(), err)
	}
	if !strings.Contains(out.String(), "Created session Second") || strings.Contains(out.String(), "Choose a number") {
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
	set := setCommand(factory, &name)
	set.SetOut(&out)
	set.SetErr(&out)
	set.SetIn(strings.NewReader(""))
	set.SetArgs([]string{q.Workspace, "--name", "Second"})
	if err := set.ExecuteContext(context.Background()); err != nil {
		t.Fatal(out.String(), err)
	}
	if selected, err := e.Locate(context.Background(), q.Workspace, ""); err != nil || selected.ID != r.ID {
		t.Fatal("set did not select the requested session", selected.ID, err)
	}
}

func TestInteractiveCreationStartsWithBlankNameAndOnlySelectsConfigs(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if _, err := master.WriteString("Fresh\n1\n1\n1\n"); err != nil {
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
	if !strings.HasPrefix(text, "Session name (:back cancels): ") || !strings.Contains(text, "Created session Fresh") {
		t.Fatal(text)
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("interactive creation selected a default", selected, err)
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
