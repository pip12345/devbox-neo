package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

func TestCreateDefaultFlagSelectsCreatedSession(t *testing.T) {
	e, q, original := namedCLIFixture(t)
	ctx := context.Background()
	if err := e.SetDefault(ctx, sessionRecord(t, e, original)); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := createCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{q.Workspace, "--name", "New", "--config", "base", "--default"})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	created, err := e.Locate(ctx, q.Workspace, "")
	if err != nil || created.Settings.LocalName != "New" {
		t.Fatal(created, err)
	}
	if !strings.Contains(out.String(), "Folder default: New") || !strings.Contains(out.String(), "dbx open "+created.Directory) || strings.Contains(out.String(), "Select it as") {
		t.Fatal("misleading creation receipt", out.String())
	}
}

func TestCreationDefaultToggleCanBeCancelledWithoutChangingChoice(t *testing.T) {
	f, out, q, original := frontendFixture(t, strings.NewReader("7\n0\n"))
	if err := f.e.SetDefault(context.Background(), sessionRecord(t, f.e, original)); err != nil {
		t.Fatal(err)
	}
	p, err := newSourcePicker(f.m, f.s.Home, q.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	draft, proceed, err := sessionCreationMenu(p, f.e, sessionCreationDraft{workspace: q.Workspace, name: "New", sources: q.Sources}, func(sessionCreationDraft) (bool, error) { t.Fatal("cancel submitted creation"); return false, nil })
	if err != nil || proceed || !draft.makeDefault {
		t.Fatal(draft, proceed, err, out.String())
	}
	if !strings.Contains(out.String(), "Replaces Main") || !strings.Contains(out.String(), "✓ Make folder default") {
		t.Fatal("toggle or replacement description missing", out.String())
	}
	if selected, err := f.e.Store.ReadDefault(context.Background(), q.Workspace); err != nil || selected == nil || selected.ID != sessionRecord(t, f.e, original).ID {
		t.Fatal("draft changed saved default", selected, err)
	}
}

func TestCreationDefaultFailureOpensSavedSessionInsteadOfRetryingCreate(t *testing.T) {
	f, out, q, _ := frontendFixture(t, strings.NewReader("1\nNew\n2\n1\n7\n8\n0\n"))
	d := f.e.Docker.Runner.(*dockertest.Daemon)
	d.Fail = func(args []string) error {
		if args[0] == "stop" {
			return os.WriteFile(filepath.Join(f.e.Store.Home, "state/folder-defaults.json"), []byte("broken"), 0600)
		}
		return nil
	}
	if err := f.createSessionIn(q.Workspace); err != nil {
		t.Fatal(err, out.String())
	}
	created, err := f.e.Locate(context.Background(), q.Workspace, "New")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Session · New") || !strings.Contains(out.String(), "dbx edit "+created.Directory+" --default") {
		t.Fatal("post-creation failure lost saved session or repair guidance", out.String())
	}
}

func TestNativeCreationDefaultCheckbox(t *testing.T) {
	e, q, original := namedCLIFixture(t)
	if err := e.SetDefault(context.Background(), sessionRecord(t, e, original)); err != nil {
		t.Fatal(err)
	}
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
		return f.createSessionFromDraft(sessionCreationDraft{workspace: q.Workspace, name: "New", sources: q.Sources})
	})
	p.wait("○ Make folder default")
	if !strings.Contains(p.output(), "Replaces Main") {
		t.Fatal("replacement was not shown", p.output())
	}
	p.send("\x1b[F\x1b[A\r")
	p.wait("✓ Make folder default")
	if selected, err := e.Store.ReadDefault(context.Background(), q.Workspace); err != nil || selected == nil || selected.ID != sessionRecord(t, e, original).ID {
		t.Fatal("checkbox saved before creation", selected, err)
	}
	p.send("\x1b[F\r")
	p.wait("Press Enter")
	p.send("\r")
	p.wait("Session · New")
	selected, err := e.Locate(context.Background(), q.Workspace, "")
	if err != nil || selected.Settings.LocalName != "New" {
		t.Fatal("checked creation did not select new session", selected, err)
	}
	p.send("q")
	p.finish(done)
}
