package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"devbox/internal/app"
	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
)

func TestSessionMenuExposesOperationsDirectly(t *testing.T) {
	f, out, _, name := frontendFixture(t, strings.NewReader("0\n"))
	if err := f.session(app.View{Target: name}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, label := range []string{"Start", "Stop", "Recreate", "Logs", "Networks", "Exec", "SSH", "Open with options"} {
		if !strings.Contains(text, label) {
			t.Fatal("missing direct operation", label, text)
		}
	}
	for _, label := range []string{"Container actions", "Exec, SSH and launch arguments", "Default:"} {
		if strings.Contains(text, label) {
			t.Fatal("obsolete menu or detail", label, text)
		}
	}
}
func TestSessionActivityIsRelativeInBothPanes(t *testing.T) {
	f, _, q, name := frontendFixture(t, strings.NewReader(""))
	v := app.View{Target: name, LocalName: "Main", Workspace: q.Workspace, LastActivity: time.Now().Add(-3 * 24 * time.Hour), Default: true}
	c := f.sessionCollection(app.InventoryReport{Sessions: []app.View{v}}, "name")
	item := c.Items[1]
	if item.Activity != "3 days ago" || !item.Selected {
		t.Fatal(item)
	}
	found := false
	for _, field := range item.Fields {
		if field.Label == "Default" {
			t.Fatal("redundant default detail", field)
		}
		if field.Label == "Last active" {
			found = true
			if field.Value != item.Activity {
				t.Fatal(field, item.Activity)
			}
		}
	}
	if !found {
		t.Fatal("missing activity preview")
	}
}
func TestDeleteScopeControlsPreviewAndExecution(t *testing.T) {
	for _, whole := range []bool{false, true} {
		input := ""
		if whole {
			input = "1\n2\n"
		}
		input += "3\n0\n4\ny\n"
		if whole {
			input += "y\n"
		}
		f, out, q, name := frontendFixture(t, strings.NewReader(input))
		containerName := sessionRecord(t, f.e, name).Applied.Creation.Name
		if err := f.delete([]string{name}); err != nil {
			t.Fatal(err, out.String())
		}
		text := out.String()
		label := "Container only"
		if whole {
			label = "Container and saved data/history"
		}
		for _, want := range []string{"Delete · Main", "Folder: " + q.Workspace, "Delete scope: " + label, "Force: Off", "Would delete container " + containerName} {
			if !strings.Contains(text, want) {
				t.Fatal("missing visible deletion choice", want, text)
			}
		}
		if strings.Contains(text, "Preview scope") || strings.Contains(text, "Delete saved data and history?") != whole || strings.Contains(text, "Would delete session "+name) != whole {
			t.Fatal("preview and execution disagree", text)
		}
		_, err := f.e.Store.Find(context.Background(), name, nil)
		if os.IsNotExist(err) != whole {
			t.Fatal("wrong saved-state outcome", whole, err, text)
		}
		report, err := f.e.List(context.Background(), "")
		if err != nil || (!whole && report.Sessions[0].Exists) {
			t.Fatal(report, err)
		}
	}
}

func TestNativeRecreateRefreshesNavigationAndRetainsFailedForm(t *testing.T) {
	for _, fail := range []bool{false, true} {
		nameCase := "missing to stopped"
		if fail {
			nameCase = "failed recreate updates missing"
		}
		t.Run(nameCase, func(t *testing.T) {
			e, _, name := namedCLIFixture(t)
			d := e.Docker.Runner.(*dockertest.Daemon)
			if fail {
				d.Fail = func(args []string) error {
					if len(args) > 0 && args[0] == "create" {
						return errors.New("injected create failure")
					}
					return nil
				}
			} else {
				forgetSession(t, e, name)
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
				return f.session(app.View{Target: name})
			})
			p.wait("Session · Main")
			p.send("/Recreate\r\r")
			p.wait("Inspect pending changes")
			if fail {
				p.send("\x1b[B\r")
			}
			p.send("\x1b[F\r")
			p.wait("Confirm action")
			p.send("\x1b[C\r")
			p.wait("Press Enter")
			mark := len(p.output())
			p.send("\r")
			if fail {
				p.wait("injected create failure")
				rendered := ansi.Strip(p.output()[mark:])
				if !strings.Contains(rendered, "missing") || !strings.Contains(rendered, "✓") {
					t.Fatal("failed form lost inputs or kept stale inventory", rendered)
				}
				p.send("q")
			} else {
				p.wait("Session · Main")
				if !strings.Contains(ansi.Strip(p.output()[mark:]), "stopped") {
					t.Fatal("successful recreate kept missing state", p.output()[mark:])
				}
			}
			// Clear the parent action filter, then leave the session menu.
			p.send("qq")
			p.finish(done)
			report, err := e.List(context.Background(), "")
			if err != nil || len(report.Sessions) != 1 || report.Sessions[0].Exists == fail {
				t.Fatal(report, err)
			}
		})
	}
}
