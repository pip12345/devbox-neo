package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/config"
	"devbox/internal/environment"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

func createSessionMenu(p sourcePicker, e *app.Engine, draft sessionCreationDraft) (sessionCreationDraft, bool, error) {
	return sessionCreationMenu(p, e, draft, func(sessionCreationDraft) (bool, error) { return true, nil })
}

func TestCreateSessionAlwaysVisibleAndReportsMissingInputs(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	for _, tc := range []struct {
		name    string
		draft   sessionCreationDraft
		message string
	}{
		{"both", sessionCreationDraft{workspace: q.Workspace}, "Set a session name and add at least one config first."},
		{"config", sessionCreationDraft{workspace: q.Workspace, name: "Work"}, "Add at least one config first."},
		{"name", sessionCreationDraft{workspace: q.Workspace, sources: q.Sources}, "Set a session name first."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			choice := 6
			if len(tc.draft.sources) > 0 {
				choice = 8
			}
			m := testMenu(context.Background(), strings.NewReader(fmt.Sprintf("%d\n0\n", choice)), &out)
			p, err := newSourcePicker(m, e.Store.Home, q.Workspace)
			if err != nil {
				t.Fatal(err)
			}
			after, proceed, err := createSessionMenu(p, e, tc.draft)
			if err != nil || proceed || !reflect.DeepEqual(after, tc.draft) {
				t.Fatal(after, proceed, err)
			}
			text := out.String()
			if strings.Count(text, menuPrefix(choice)+"Create session") != 2 || !strings.Contains(text, tc.message) {
				t.Fatal(text)
			}
		})
	}
}

func TestConfigCreationReusesStandaloneSetupAndPreservesDraft(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	var nested bytes.Buffer
	m := testMenu(context.Background(), strings.NewReader("3\n1\nfresh\n2\n1\n3\n2\n5\n4\n0\n"), &nested)
	p, err := newSourcePicker(m, e.Store.Home, q.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	draft := sessionCreationDraft{workspace: q.Workspace, name: "Work", sources: q.Sources}
	after, proceed, err := createSessionMenu(p, e, draft)
	if err != nil || proceed || after.name != "Work" || len(after.sources) != len(draft.sources)+1 {
		t.Fatal(after, proceed, err, nested.String())
	}
	fresh := filepath.Join(e.Store.Home, "configs", "fresh")
	configBytes, err := os.ReadFile(filepath.Join(fresh, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	setupBytes, err := os.ReadFile(filepath.Join(fresh, "setup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := environment.Identify(q.Workspace, "Work")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.Find(context.Background(), "", &identity.Binding); !os.IsNotExist(err) {
		t.Fatal("cancelling created a session", err)
	}

	// Exercise the actual config create command with the same setup choices.
	master, slave := testTerminal(t)
	if _, err := master.WriteString("2\n1\n3\n2\n5\n4\n"); err != nil {
		t.Fatal(err)
	}
	service := &resource.Service{Home: e.Store.Home}
	cmd := directoryCommand(func(*cobra.Command) (*resource.Service, error) { return service, nil }, true)
	var standalone bytes.Buffer
	cmd.SetIn(slave)
	cmd.SetOut(&standalone)
	cmd.SetErr(&standalone)
	cmd.SetArgs([]string{"separate"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err, standalone.String())
	}
	for _, file := range []struct {
		name string
		want []byte
	}{{"config.json", configBytes}, {"setup.sh", setupBytes}} {
		data, err := os.ReadFile(filepath.Join(e.Store.Home, "configs", "separate", file.name))
		if err != nil || !bytes.Equal(data, file.want) {
			t.Fatal("standalone and nested setup differ", file.name, err)
		}
	}
	for _, screen := range []string{"Create config", "Name/location:", "Optional files:", "Select a harness", "Choose optional files", "Current selection:"} {
		if !strings.Contains(nested.String(), screen) || !strings.Contains(standalone.String(), screen) {
			t.Fatal("creation entry points did not share screens", screen)
		}
	}
	if strings.Contains(nested.String(), "dbx create <folder>") {
		t.Fatal("nested creation printed standalone next steps", nested.String())
	}
}

func TestCancelledAndFailedConfigSetupLeaveParentDraftUntouched(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"name-back", "3\n1\n:back\n0\n0\n"},
		{"setup-cancel", "3\n1\nfresh\n0\n0\n"},
		{"optional-back", "3\n1\nfresh\n3\n2\n0\n0\n0\n"},
		{"existing-config", "3\n1\nbase\n:back\n0\n0\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, q, _ := namedCLIFixture(t)
			var out bytes.Buffer
			m := testMenu(context.Background(), strings.NewReader(tc.input), &out)
			p, err := newSourcePicker(m, e.Store.Home, q.Workspace)
			if err != nil {
				t.Fatal(err)
			}
			before := sessionCreationDraft{workspace: q.Workspace, name: "Work", sources: q.Sources}
			after, proceed, err := createSessionMenu(p, e, before)
			if err != nil || proceed || !reflect.DeepEqual(before, after) {
				t.Fatal(after, proceed, err, out.String())
			}
			if _, err := os.Stat(filepath.Join(e.Store.Home, "configs", "fresh", "config.json")); !os.IsNotExist(err) {
				t.Fatal("cancelled setup created a config", err)
			}
			if tc.name == "existing-config" && (!strings.Contains(out.String(), "Config already exists") || strings.Contains(out.String(), "Select a harness")) {
				t.Fatal(out.String())
			}
		})
	}
}

func TestEmptyPickerCreatesNamedAndRelativeConfigsInSelectedHome(t *testing.T) {
	for _, input := range []string{"fresh", "./fresh"} {
		t.Run(input, func(t *testing.T) {
			s := menuService(t)
			cwd, workspace := t.TempDir(), t.TempDir()
			var out bytes.Buffer
			p := sourcePicker{menu: testMenu(context.Background(), strings.NewReader("2\n1\n"+input+"\n2\n1\n4\n"), &out), home: s.Home, cwd: cwd, workspace: workspace, userHome: t.TempDir()}
			ref, selected, err := p.choose(nil, "Back")
			if err != nil || !selected {
				t.Fatal(ref, selected, err, out.String())
			}
			wantPath := filepath.Join(s.Home, "configs", "fresh")
			wantKind := config.ReferenceFixed
			if input == "./fresh" {
				wantPath = filepath.Join(cwd, "fresh")
				wantKind = config.ReferenceRelative
			}
			resolved, err := ref.Expand(workspace)
			if err != nil || resolved.Path != wantPath || ref.Kind != wantKind {
				t.Fatal(ref, resolved, err)
			}
			if _, err := os.Stat(filepath.Join(wantPath, "config.json")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReorderActionRequiresTwoConfigs(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	s := resource.Service{Home: e.Store.Home}
	owner, err := s.ConfigDirectory("second", q.Workspace, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateConfig(context.Background(), owner, resource.SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{0, 1, 2} {
		var out bytes.Buffer
		p, err := newSourcePicker(testMenu(context.Background(), strings.NewReader("0\n"), &out), e.Store.Home, q.Workspace)
		if err != nil {
			t.Fatal(err)
		}
		var sources []config.Reference
		for _, name := range []string{"base", "second"}[:count] {
			r, err := p.capture(name)
			if err != nil {
				t.Fatal(err)
			}
			sources = append(sources, r)
		}
		_, _, err = createSessionMenu(p, e, sessionCreationDraft{workspace: q.Workspace, name: "Work", sources: sources})
		if err != nil || strings.Contains(out.String(), "Reorder configs") != (count == 2) {
			t.Fatal(count, err, out.String())
		}
	}
}

type workflowInput struct {
	lines  []string
	next   int
	before func(int)
}

func (r *workflowInput) Read(p []byte) (int, error) {
	if r.next == len(r.lines) {
		return 0, io.EOF
	}
	if r.before != nil {
		r.before(r.next)
	}
	line := r.lines[r.next]
	r.next++
	return copy(p, line), nil
}
func TestCombinedConfigurationHasOwnBackOnlyScreen(t *testing.T) {
	e, _, name := namedCLIFixture(t)
	record, err := e.Store.Find(context.Background(), name, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	input := &workflowInput{lines: []string{"5\n", "0\n", "0\n"}}
	input.before = func(step int) {
		if step != 1 {
			return
		}
		text := out.String()
		view := strings.LastIndex(text, "Combined configuration")
		if view < 0 {
			t.Fatal("missing combined view", text)
		}
		frame := text[view:]
		if !strings.Contains(frame, "base_image:") || !strings.Contains(frame, "[0]  Back") || strings.Contains(frame, "Manage configs") || strings.Contains(frame, "[1]") {
			t.Fatal("view mixed parent actions with its content", frame)
		}
	}
	saved, err := sourceChainMenu(testMenu(context.Background(), input, &out), e, record, "Exit")
	if err != nil || saved || strings.Count(out.String(), "Manage configs") != 2 {
		t.Fatal(saved, err, out.String())
	}
	after, err := e.Store.Find(context.Background(), name, nil)
	if err != nil || !reflect.DeepEqual(record, after) {
		t.Fatal("inspection changed saved state", err)
	}
}
