package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/app"
	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
)

func TestStatusScreenColorsResultsButNotReasons(t *testing.T) {
	for _, tt := range []struct {
		name                    string
		view                    app.View
		success, pending, error bool
	}{
		{"clean", app.View{Desired: environment.NoChange}, true, false, false},
		{"runtime", app.View{Desired: environment.RuntimeSync}, false, true, false},
		{"container", app.View{Desired: environment.Recreate}, false, true, false},
		{"image", app.View{Desired: environment.RebuildAndRecreate}, false, true, false},
		{"invalid", app.View{ConfigError: "bad config"}, false, false, true},
		{"broken", app.View{Error: "bad record"}, false, false, true},
		{"unknown", app.View{}, false, false, true},
		{"transfer", app.View{Pending: &store.Reservation{Mode: "clone", Phase: "prepare"}}, false, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.view.Exists = true
			tt.view.PendingInputChanges = []environment.InputChange{{Scope: environment.ContainerScope, Field: "network", Code: "input_changed"}}
			fields := statusViewFields(tt.view, "Changes")
			result := fields[0]
			if result.Value != statusChange(tt.view) || result.Success != tt.success || result.Attention != tt.pending || result.Warning != tt.error {
				t.Fatal("incorrect result color", result)
			}
			if fields[1].Label != "Container" || len(fields[1].Values) != 1 || fields[1].Values[0] != "network changed" || fields[1].Success || fields[1].Attention || fields[1].Warning {
				t.Fatal("reason colored or missing", fields)
			}
			for _, field := range fields {
				if field.Group != "Changes" {
					t.Fatal("missing field section", field)
				}
			}
		})
	}
}

func TestStatusScreenKeepsAllDetailsAndScopes(t *testing.T) {
	e, _, target := namedCLIFixture(t)
	details, err := e.Status(context.Background(), target, "")
	if err != nil {
		t.Fatal(err)
	}
	details.Desired = environment.RebuildAndRecreate
	details.PendingInputChanges = []environment.InputChange{
		{Scope: environment.ImageScope, Field: "harness_definition", Code: "file_content_changed"},
		{Scope: environment.ContainerScope, Field: "network", Code: "input_changed"},
		{Scope: environment.RuntimeScope, Field: "managed_config", Code: "input_changed"},
	}
	details.ImageMissing = true
	details.DefaultError = "default unavailable"
	details.Active = []store.Lease{{Action: "shell", Process: store.Process{PID: 1234}, Created: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}}
	details.Record.Applied.Inputs.Container.Workspace = "/old/folder"
	steps := []commanderror.Step{commanderror.Next("To apply changes", "--home", "/custom home", "recreate", target)}
	page := statusDetailsScreen(details, steps)
	if page.Body != nil || len(page.Actions) != 0 || page.Back != "Back" {
		t.Fatal("status is not a structured read-only screen", page)
	}
	var text strings.Builder
	for _, field := range page.Fields {
		fmt.Fprintln(&text, field.Group, field.Label, field.Value, field.Values)
	}
	for _, want := range []string{"Changes", "Next step", "Session", "Runtime", "Active commands", "Rebuild + recreate needed", "harness definition changed", "network changed", "managed configuration changed", "dbx --home '/custom home' recreate " + target, details.StoragePath, details.ContainerID, details.Record.Applied.ImageID, "/old/folder", "host PID 1234", "2026-03-01T12:00:00Z", "default unavailable", "Existing container access is unaffected"} {
		if !strings.Contains(text.String(), want) {
			t.Fatal("status lost details", want, text.String())
		}
	}
	if strings.Contains(text.String(), ";") || strings.Contains(text.String(), "more in Status") {
		t.Fatal("status hid reasons or used unwanted wording", text.String())
	}
}

func TestStatusScreenRecreateCommandsMatchCLI(t *testing.T) {
	for _, view := range []app.View{
		{Exists: true, Desired: environment.NoChange},
		{Exists: true, Desired: environment.RuntimeSync},
		{Exists: true, Desired: environment.Recreate},
		{Exists: true, Desired: environment.RebuildAndRecreate},
		{ConfigError: "repair config", Desired: environment.NoChange},
		{Exists: true, ConfigError: "repair config"},
		{Exists: true, ImageMissing: true, Desired: environment.NoChange},
		{Error: "broken state"},
		{Pending: &store.Reservation{Mode: "clone", Phase: "committed"}},
	} {
		fields := statusStepFields(view, []commanderror.Step{commanderror.Next("Apply", "recreate", "dbx-api-111111111111.main")}, "Next step")
		if (len(fields) > 0) != statusNeedsRecreate(view) {
			t.Fatal("screen and CLI disagree about next steps", view, fields)
		}
	}
}

func TestBulkStatusScreenKeepsDiagnosticsWithoutWideTable(t *testing.T) {
	f, _, _, _ := frontendFixture(t, strings.NewReader(""))
	report := app.InventoryReport{
		Sessions: []app.View{
			{Target: "first-target", Workspace: "/first", LocalName: "work", Exists: true, Running: true, Desired: environment.NoChange},
			{Target: "second-target", Workspace: "/second", LocalName: "work", Exists: true, Desired: environment.Recreate, ConfigError: "bad config"},
		},
		DefaultErrors:       map[string]string{"/second": "missing default", "/first": "invalid default"},
		UnmatchedContainers: []app.View{{Target: "unmatched", Exists: true, Running: true}},
	}
	page := f.statusOverviewScreen(report)
	var text strings.Builder
	for _, field := range page.Fields {
		fmt.Fprintln(&text, field.Group, field.Label, field.Value)
	}
	for _, want := range []string{"/first / work", "/second / work", "first-target", "second-target", "No changes", "bad config", "invalid default", "missing default", "Unmatched containers", "unmatched", "No saved session record"} {
		if !strings.Contains(text.String(), want) {
			t.Fatal("bulk status lost details", want, text.String())
		}
	}
	if page.Body != nil || strings.Index(text.String(), "invalid default") > strings.Index(text.String(), "missing default") {
		t.Fatal("bulk view is unstructured or defaults are unsorted", text.String())
	}
}

func TestNativeStatusScreenShowsGroupedChangesAndReturns(t *testing.T) {
	e, _, target := namedCLIFixture(t)
	config := filepath.Join(e.Store.Home, "configs/base/config.json")
	if err := os.WriteFile(config, []byte(`{"version":1,"harness":"pi","network":"host","harness_args":["--verbose"]}`), 0600); err != nil {
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
		return f.status(target)
	})
	p.wait("Recreate needed")
	view := ansi.Strip(p.output())
	for _, want := range []string{"Changes", "network:", "harness arguments", "Next step", "Command"} {
		if !strings.Contains(view, want) {
			t.Fatal("native status lost structure or reasons", want, view)
		}
	}
	p.send("\x1b[F")
	if !strings.Contains(ansi.Strip(p.output()), "Active commands") {
		t.Fatal("status details are unreachable", p.output())
	}
	p.send("q")
	p.finish(done)
}
