package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/resource"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
)

func TestNativeFirstCreationAndSessionDefaultMenu(t *testing.T) {
	e, q, old := namedCLIFixture(t)
	if _, err := e.Delete(context.Background(), app.DeleteOptions{Selection: app.Selection{Targets: []string{old}}, Scope: app.DeleteSession}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(e.Store.Home, "configs", "base")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(q.Workspace)
	p := newTerminalProbe(t)
	done := p.workflow(func(ctx context.Context, tty *os.File) (err error) {
		cmd := &cobra.Command{Use: "dbx"}
		cmd.SetContext(ctx)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		cmd.SetErr(tty)
		m := newMenu(cmd)
		defer func() { err = errors.Join(err, m.Finish()) }()
		f := &frontend{m: m, cmd: cmd, e: e, s: &resource.Service{Home: e.Store.Home}}
		return f.browse(false)
	})
	p.wait("No sessions yet.")
	p.send("\r")
	p.wait("Set session name")
	p.send("\r")
	p.send("First\r")
	p.send("\x1b[B\x1b[B\r")
	p.wait("Name/location")
	p.send("\r")
	p.send("first-config\r")
	p.send("\x1b[B\r")
	p.wait("Select a harness")
	p.send("/pi\r\r")
	p.send("\x1b[F\r")
	p.wait("Created config first-config.")
	p.send("\x1b[F\r")
	p.wait("Press Enter")
	mark := len(p.output())
	p.send("\r")
	p.wait("Session · First")
	if view := ansi.Strip(p.output()[mark:]); !strings.Contains(view, "▸ Continue") {
		t.Fatal("new session did not start with Continue selected", view)
	}
	created, err := e.Locate(context.Background(), q.Workspace, "First")
	if err != nil {
		t.Fatal(err)
	}
	name := created.ID
	selected, err := e.Store.ReadDefault(context.Background(), q.Workspace)
	if err != nil || selected != nil {
		t.Fatal("creation selected a default", selected, err)
	}
	report, err := e.List(context.Background(), "")
	if err != nil || len(report.Sessions) != 1 || report.Sessions[0].Target != name || report.Sessions[0].Running {
		t.Fatal(report, err)
	}
	p.send("/Make folder\r\r")
	p.wait("Clear folder default")
	selected, err = e.Store.ReadDefault(context.Background(), q.Workspace)
	if err != nil || selected == nil || selected.ID != name {
		t.Fatal("session menu did not set default", selected, err)
	}
	p.send("\r")
	p.wait("Make folder default")
	selected, err = e.Store.ReadDefault(context.Background(), q.Workspace)
	if err != nil || selected != nil {
		t.Fatal("same menu did not clear default", selected, err)
	}
	p.send("q")
	p.wait("Browser actions")
	p.send("q")
	p.finish(done)
}
