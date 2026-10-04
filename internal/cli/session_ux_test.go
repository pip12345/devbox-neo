package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

func TestSessionMenuDeclaresLocalShortcuts(t *testing.T) {
	f, _, q, name := frontendFixture(t, strings.NewReader(""))
	v := app.View{Target: name, SessionID: name, LocalName: "Main", Workspace: q.Workspace, Harness: "pi", Exists: true, Running: true, ManualStart: true}
	page := f.sessionScreen(v, nil)
	wantKeys := map[string]string{"Continue": "c", "Open": "o", "Shell": "s", "Recreate": "r", "Status": "i", "Logs": "l", "Exec": "e"}
	seen := map[string]bool{}
	for _, a := range page.Actions {
		if a.Shortcut != wantKeys[a.Label] {
			t.Fatal("wrong shortcut", a.Label, a.Shortcut)
		}
		if a.Shortcut != "" {
			if seen[a.Shortcut] {
				t.Fatal("duplicate shortcut", a.Shortcut)
			}
			seen[a.Shortcut] = true
		}
		if a.Run == nil || a.Description == "" {
			t.Fatal("action lost its handler or explanation", a.Label)
		}
		if a.Label == "Start" && a.Description != "Keep running until explicitly stopped" {
			t.Fatal("Start lost its lifetime explanation", a.Description)
		}
		if strings.Contains(a.Label, "…") || strings.Contains(a.Label, "...") {
			t.Fatal("menu depth is still indicated by punctuation", a.Label)
		}
	}
	if len(page.Actions) != 18 || len(seen) != len(wantKeys) {
		t.Fatal("session capabilities or frequent shortcuts disappeared", len(page.Actions), seen)
	}
	if page.Summary[2].Value != "until stop" || len(page.Fields) != 1 || page.Fields[0].Value != q.Workspace {
		t.Fatal("compact context lost lifetime or target", page)
	}
	collection := f.sessionCollection(app.InventoryReport{Sessions: []app.View{v}}, "name")
	if len(collection.Items[0].Actions) != 2 || len(collection.Items[1].Actions) != len(page.Actions) {
		t.Fatal("browser preview did not use the folder/session action owners", collection.Items)
	}
	for i, a := range collection.Items[1].Actions {
		if a.Label != page.Actions[i].Label || a.Shortcut != page.Actions[i].Shortcut {
			t.Fatal("preview and interactive session menu disagree", a, page.Actions[i])
		}
	}
	v.LocalName, v.Default = "None", true
	collection = f.sessionCollection(app.InventoryReport{Sessions: []app.View{v}}, "name")
	if collection.Items[0].Actions[1].Hidden {
		t.Fatal("default display text was mistaken for absence of a saved selection")
	}
}

func TestNativeSessionHotkeysKeepTargetAndRecreateConfirmation(t *testing.T) {
	e, q, first := namedCLIFixture(t)
	q.LocalName = "Second"
	second, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(q.Sources[0].Path+"/config.json", []byte(`{"version":1,"harness":"pi","network":"host"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(q.Workspace)
	d := e.Docker.Runner.(*dockertest.Daemon)
	firstContainer := sessionRecord(t, e, first).Applied.SetupContainer
	secondContainer := sessionRecord(t, e, second.SessionID).Applied.SetupContainer
	attached := make(chan docker.Command, 2)
	d.Attached = func(_ context.Context, c docker.Command) error {
		if c.Stdin != nil {
			attached <- c
			fmt.Fprintln(c.Stdout, "HOTKEY ATTACHMENT COMPLETE")
		}
		return nil
	}
	p := newTerminalProbe(t)
	done := p.workflow(func(ctx context.Context, tty *os.File) (err error) {
		cmd := &cobra.Command{Use: "dbx"}
		cmd.SetContext(ctx)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		cmd.SetErr(tty)
		e.Streams = docker.Streams{In: tty, Out: tty, Err: tty, TTY: true}
		m := newMenu(cmd)
		defer func() { err = errors.Join(err, m.Finish()) }()
		f := &frontend{m: m, cmd: cmd, e: e, s: &resource.Service{Home: e.Store.Home}}
		return f.browse(false)
	})
	p.wait("Browser actions")
	p.send("\r")
	p.wait("Session · Main")
	p.send("r")
	p.wait("Inspect pending changes")
	p.send("r")
	p.wait("Confirm action")
	// Neither the action hotkey nor Enter on default No may apply config.
	p.send("r\r")
	p.wait("Inspect pending changes")
	if got := sessionRecord(t, e, first).Applied.SetupContainer; got != firstContainer {
		t.Fatal("Recreate shortcut bypassed confirmation", got, firstContainer)
	}
	p.send("q")
	p.wait("Session · Main")
	p.send("c")
	p.wait("Press Enter")
	check := func(container string) {
		t.Helper()
		select {
		case c := <-attached:
			if !slices.Contains(c.Args, container) || c.Args[len(c.Args)-1] != "-c" {
				t.Fatal("Continue hotkey launched a different target or lost continuation", c.Args)
			}
		default:
			t.Fatal("Continue hotkey did not run an attachment")
		}
	}
	check(firstContainer)
	p.send("\r")
	p.wait("Session · Main")
	p.send("q")
	p.wait("Browser actions")
	p.send("\x1b[B\r")
	p.wait("Session · Second")
	p.send("c")
	p.wait("Press Enter")
	check(secondContainer)
	p.send("\r")
	p.wait("Session · Second")
	p.send("q")
	p.wait("Browser actions")
	p.send("q")
	p.finish(done)
}
