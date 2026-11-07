package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/docker/dockertest"
	"devbox/internal/environment"
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
)

func TestSessionPendingChangePresentation(t *testing.T) {
	for _, tt := range []struct {
		name, label string
		view        app.View
	}{
		{name: "unchanged", view: app.View{Desired: environment.NoChange}},
		{name: "unchecked", view: app.View{}},
		{name: "missing image alone", view: app.View{Desired: environment.NoChange, ImageMissing: true}},
		{name: "runtime", label: "Update pending", view: app.View{Desired: environment.RuntimeSync}},
		{name: "container", label: "Recreate needed", view: app.View{Desired: environment.Recreate}},
		{name: "image", label: "Rebuild + recreate needed", view: app.View{Desired: environment.RebuildAndRecreate}},
		{name: "invalid config", view: app.View{Desired: environment.Recreate, ConfigError: "invalid config"}},
		{name: "broken state", view: app.View{Desired: environment.Recreate, Error: "broken record"}},
		{name: "transfer", view: app.View{Desired: environment.Recreate, Pending: &store.Reservation{}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.view
			v.Target, v.SessionID, v.LocalName, v.Workspace = "work", "abcdef012345", "work", "/project"
			v.Exists, v.Running, v.Default = true, true, true
			v.PendingInputChanges = []environment.InputChange{{Scope: environment.ContainerScope, Field: "network", Code: "input_changed"}}
			f := frontend{}
			item := f.sessionCollection(app.InventoryReport{Sessions: []app.View{v}}, "name").Items[1]
			if item.PendingChanges != (tt.label != "") || item.Label != "work" || item.Key != v.SessionID || !item.Selected {
				t.Fatal("pending indication changed identity or default", item)
			}
			if v.Error == "" && v.Pending == nil && item.Status != "running" {
				t.Fatal("config check replaced running status", item.Status)
			}
			fields := sessionChangeFields(v)
			if tt.label != "" {
				if len(fields) != 2 || fields[0].Value != "↻ "+tt.label || !fields[0].Attention || fields[0].Warning || fields[1].Value != "network changed" || fields[1].Attention || fields[1].Warning {
					t.Fatal("notice or normal-text reason incorrect", fields)
				}
			} else if v.ConfigError != "" {
				if len(fields) != 1 || fields[0].Value != v.ConfigError || !fields[0].Warning {
					t.Fatal("invalid config was hidden", fields)
				}
			} else if len(fields) != 0 {
				t.Fatal("unchecked or unusable state claims pending changes", fields)
			}
			page := f.sessionScreen(v, nil)
			if page.Title != item.Label || !reflect.DeepEqual(page.Fields, item.Fields) || !reflect.DeepEqual(page.Summary, item.Summary) {
				t.Fatal("focus changed session title or context", page.Title, item.Label, page.Fields, item.Fields)
			}
			for _, action := range page.Actions {
				if (action.Label == "Continue" || action.Label == "Open" || action.Label == "Recreate") && action.Blocked != "" {
					t.Fatal("pending changes block existing actions", action)
				}
			}
		})
	}
}

func TestNativePendingChangesClearAfterRecreate(t *testing.T) {
	e, _, name := namedCLIFixture(t)
	if err := os.WriteFile(filepath.Join(e.Store.Home, "configs/base/config.json"), []byte(`{"version":1,"harness":"pi","network":"host"}`), 0600); err != nil {
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
		f := frontend{m: m, cmd: cmd, e: e, s: &resource.Service{Home: e.Store.Home}}
		return f.session(app.View{Target: name})
	})
	p.wait("↻ Recreate needed")
	view := ansi.Strip(p.output())
	if !strings.Contains(view, "Main ↻") || !strings.Contains(view, "network:") {
		t.Fatal("native menu lost marker or reason", view)
	}
	p.send("/Recreate\r\r")
	p.wait("Inspect pending changes")
	p.send("\x1b[F\r")
	p.wait("Confirm action")
	p.send("\x1b[C\r")
	p.wait("Press Enter")
	mark := len(p.output())
	p.send("\r")
	p.wait("Main ·")
	if view := ansi.Strip(p.output()[mark:]); strings.Contains(view, "↻") {
		t.Fatal("applied changes retained pending indicator", view)
	}
	p.send("qq")
	p.finish(done)
}

func TestSessionPendingReasonsStayCompact(t *testing.T) {
	fields := sessionChangeFields(app.View{Desired: environment.Recreate, PendingInputChanges: []environment.InputChange{
		{Field: "network", Code: "input_changed"},
		{Field: "env", Key: "TOKEN", Code: "entry_added"},
	}})
	if len(fields) != 2 || fields[1].Value != "network changed (+1 more in Status)" {
		t.Fatal(fields)
	}
}

func TestSessionPendingChangesClearAfterExplicitRecreate(t *testing.T) {
	for _, tt := range []struct {
		name, config, label string
		replaced            bool
	}{
		{"runtime", `{"version":1,"harness":"pi","harness_args":["--verbose"]}`, "Update pending", false},
		{"container", `{"version":1,"harness":"pi","network":"host"}`, "Recreate needed", true},
		{"image", `{"version":1,"harness":"claude"}`, "Rebuild + recreate needed", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := &workflowInput{lines: []string{"12\n", "6\n", "y\n", "0\n"}}
			f, out, _, name := frontendFixture(t, input)
			before := sessionRecord(t, f.e, name)
			if err := os.WriteFile(filepath.Join(f.s.Home, "configs/base/config.json"), []byte(tt.config), 0600); err != nil {
				t.Fatal(err)
			}
			input.before = func(step int) {
				if step != 0 && step != 3 {
					return
				}
				text := out.String()
				current := text[strings.LastIndex(text, "\nMain\n"):]
				if strings.Contains(current, "↻ "+tt.label) != (step == 0) {
					t.Fatal("notice failed to appear or clear", step, current)
				}
			}
			if err := f.session(app.View{Target: name}); err != nil {
				t.Fatal(err, out.String())
			}
			after := sessionRecord(t, f.e, name)
			if before.ID != after.ID || (before.Applied.ContainerID != after.Applied.ContainerID) != tt.replaced {
				t.Fatal("indicator changed recreation behavior", before, after)
			}
		})
	}
}

func TestBrowserChecksConfigWithoutApplyingIt(t *testing.T) {
	f, out, _, name := frontendFixture(t, strings.NewReader("0\n"))
	before := sessionRecord(t, f.e, name)
	path, err := f.e.Store.RecordPath(before.ID)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.s.Home, "configs/base/config.json"), []byte(`{"version":1,"harness":"pi","network":"host"}`), 0600); err != nil {
		t.Fatal(err)
	}
	d := f.e.Docker.Runner.(*dockertest.Daemon)
	d.Calls = nil
	if err := f.browse(false); err != nil {
		t.Fatal(err, out.String())
	}
	if !strings.Contains(out.String(), "Main ↻") {
		t.Fatal("browser did not check pending changes", out.String())
	}
	for _, call := range d.Calls {
		if !(call[0] == "container" && (call[1] == "ls" || call[1] == "inspect")) && !(call[0] == "image" && call[1] == "ls") {
			t.Fatal("browser changed Docker", call)
		}
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(contents, after) {
		t.Fatal("browser changed saved state", err)
	}
}

func TestSessionMaintenanceSurvivesConfigCheckFailures(t *testing.T) {
	for _, imageFailure := range []bool{false, true} {
		f, out, _, name := frontendFixture(t, strings.NewReader("10\n0\n"))
		want := "Config error"
		if imageFailure {
			want = "Could not refresh session status"
			f.e.Docker.Runner.(*dockertest.Daemon).Fail = func(args []string) error {
				if len(args) > 1 && args[0] == "image" && args[1] == "ls" {
					return errors.New("image inventory unavailable")
				}
				return nil
			}
		} else if err := os.WriteFile(filepath.Join(f.s.Home, "configs/base/config.json"), []byte("broken"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := f.session(app.View{Target: name}); err != nil {
			t.Fatal(err, out.String())
		}
		if !strings.Contains(out.String(), want) || !sessionRecord(t, f.e, name).Settings.ManualStart {
			t.Fatal("check failure hid diagnostics or blocked Start", out.String())
		}
	}
}

func TestForegroundRefreshUpdatesPendingChangeNavigation(t *testing.T) {
	f, out, _, name := frontendFixture(t, strings.NewReader("1\n0\n"))
	report, err := f.e.StatusAll(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	collection := f.sessionCollection(report, "name")
	frame := &cliui.Navigation{Collection: *collection, Key: sessionRecord(t, f.e, name).ID}
	if err := f.m.Run(func() (cliui.Screen, error) {
		return cliui.Screen{Title: "Nested", Back: "Back", Navigation: frame, Actions: []cliui.Action{f.action("Edit config", "", func() error {
			return f.foreground("Edit config", func(context.Context) error {
				return os.WriteFile(filepath.Join(f.s.Home, "configs/base/config.json"), []byte(`{"version":1,"harness":"pi","network":"host"}`), 0600)
			})
		})}}, nil
	}); err != nil {
		t.Fatal(err, out.String())
	}
	if !frame.Collection.Items[1].PendingChanges {
		t.Fatal("foreground operation left stale navigation", frame.Collection.Items[1])
	}
}
