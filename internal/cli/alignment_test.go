package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

// Workflow tests select named actions from the presented snapshot. Rendering
// tests separately protect numbering/layout; reordering a menu is not a change
// to the persistence contract being exercised here.
type choiceScript struct {
	t       *testing.T
	out     *bytes.Buffer
	steps   []string
	offset  int
	pending *strings.Reader
}

func (s *choiceScript) Read(p []byte) (int, error) {
	if s.pending == nil || s.pending.Len() == 0 {
		if len(s.steps) == 0 {
			return 0, io.EOF
		}
		step := s.steps[0]
		s.steps = s.steps[1:]
		if label, choice := strings.CutPrefix(step, "@"); choice {
			text := s.out.String()[s.offset:]
			pattern := regexp.MustCompile(`(?m)^\s*\[([0-9]+)\]\s+` + regexp.QuoteMeta(label) + `(?:\s|$)`)
			matches := pattern.FindAllStringSubmatch(text, -1)
			if len(matches) == 0 {
				s.t.Fatalf("action %q not presented: %s", label, text)
			}
			step = matches[len(matches)-1][1]
		}
		s.offset = s.out.Len()
		s.pending = strings.NewReader(step + "\n")
	}
	return s.pending.Read(p)
}

func TestConfigReplacementIsOrderedNotAdditiveAndDoesNotApply(t *testing.T) {
	e, q, name := namedCLIFixture(t)
	ctx := context.Background()
	before, _ := e.Store.Find(ctx, name, nil)
	cwd, _ := os.Getwd()
	relative, err := filepath.Rel(cwd, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out, err := runSourcesCLI(t, e, q.Workspace, "--name", q.LocalName, "--config", "unavailable,overlay", "--config", relative, "--json")
	var result selectionResult
	if err != nil || json.Unmarshal([]byte(out), &result) != nil {
		t.Fatal(out, err)
	}
	after, _ := e.Store.Find(ctx, name, nil)
	if len(after.Settings.Sources) != 2 || after.Settings.Sources[0].Label != "unavailable,overlay" || after.Settings.Sources[1].Kind != config.ReferenceRelative || !reflect.DeepEqual(result.Sources, after.Settings.Sources) {
		t.Fatal("replacement appended, split, or reordered references", out, after.Settings.Sources)
	}
	before.Settings.Sources = after.Settings.Sources
	if !reflect.DeepEqual(before, after) {
		t.Fatal("selection changed unrelated session state")
	}
	if selected, err := e.Store.ReadDefault(ctx, q.Workspace); err != nil || selected != nil {
		t.Fatal("replacement selected a default", selected, err)
	}
}

func TestConfigReplacementRejectsAmbiguityAndDuplicateAliases(t *testing.T) {
	e, q, name := namedCLIFixture(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(q.Sources[0].Path, alias); err != nil {
		t.Fatal(err)
	}
	before, _ := e.Store.Find(context.Background(), name, nil)
	for _, args := range [][]string{
		{q.Workspace, "--config", "base"},
		{name, "--config", ""},
		{name, "--config", "base", "--config", alias},
		{name, "--config", "base", "--show"},
		{name, "--config", "base", "--default"},
		{name, "--config", "base", "--clear-default"},
		{name, "--clear-configs"},
		{name, "--json"},
	} {
		if out, err := runSourcesCLI(t, e, args...); err == nil {
			t.Fatal("accepted invalid operation", args, out)
		}
		after, _ := e.Store.Find(context.Background(), name, nil)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("failed replacement changed state", args)
		}
	}
}

func TestSourceChainDraftAndSavedRemovalHaveDifferentSubmissionRules(t *testing.T) {
	e, q, name := namedCLIFixture(t)
	var out bytes.Buffer
	m := testMenu(context.Background(), strings.NewReader(""), &out)
	picker, err := newSourcePicker(m, e.Store.Home, q.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, saved := range []bool{false, true} {
		actions := picker.chainActions(q.Sources, saved, func([]config.Reference) error { return nil })
		for _, a := range actions {
			if a.Label == "Remove config" && (a.Blocked != "") != saved {
				t.Fatal("draft and saved removal confused", saved, a)
			}
			if a.Label == "Replace config" && (a.Hidden || a.Blocked != "") {
				t.Fatal("final config cannot be replaced", a)
			}
		}
	}
	r, _ := e.Store.Find(context.Background(), name, nil)
	var required *commanderror.Error
	if _, err := e.UpdateSources(context.Background(), r, nil); !errors.As(err, &required) || required.Code != "configs_required" {
		t.Fatal("service allowed bypassing the UI guard", err)
	}
}

func TestCLIAndMenuConfigReplacementHaveEquivalentResults(t *testing.T) {
	e, _, name := namedCLIFixture(t)
	if out, err := resourceCLI(t, e.Store.Home, "config", "create", "overlay", "--json"); err != nil {
		t.Fatal(out, err)
	}
	r, _ := e.Store.Find(context.Background(), name, nil)
	var out bytes.Buffer
	input := &choiceScript{t: t, out: &out, steps: []string{"@Replace config", "@base", "@overlay", "@Exit"}}
	m := testMenu(context.Background(), input, &out)
	if saved, err := sourceChainMenu(m, e, r, "Exit"); err != nil || !saved {
		t.Fatal(out.String(), saved, err)
	}
	menuResult, _ := e.Store.Find(context.Background(), name, nil)
	if _, err := e.UpdateSources(context.Background(), menuResult, r.Settings.Sources); err != nil {
		t.Fatal(err)
	}
	if out, err := runSourcesCLI(t, e, name, "--config", "overlay"); err != nil || !strings.Contains(out, "Replaced selected configs") {
		t.Fatal(out, err)
	}
	cliResult, _ := e.Store.Find(context.Background(), name, nil)
	if !reflect.DeepEqual(menuResult, cliResult) {
		t.Fatal("entry points saved different state")
	}
}

func TestStandaloneConfigInspectionAndIncompleteUsers(t *testing.T) {
	e, q, name := namedCLIFixture(t)
	s := resource.Service{Home: e.Store.Home}
	owner := testConfigOwner(t, s.Home, "base")
	t.Setenv("INSPECTION_SECRET", "do-not-emit")
	if err := os.WriteFile(filepath.Join(owner.Root, "config.json"), []byte(`{"version":1,"harness":"pi","env":["TOKEN=${env:INSPECTION_SECRET}"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	want, err := s.ShowOwner(owner)
	if err != nil {
		t.Fatal(err)
	}
	out, err := resourceCLI(t, s.Home, "config", "show", "base", "--json")
	var view resource.ConfigView
	if err != nil || json.Unmarshal([]byte(out), &view) != nil || strings.Contains(out, "do-not-emit") {
		t.Fatal(out, err)
	}
	expected, _ := json.Marshal(want)
	actual, _ := json.Marshal(view)
	if !bytes.Equal(expected, actual) {
		t.Fatal("standalone view differs from dashboard", out)
	}
	for _, ref := range []string{"base", owner.Root} {
		out, err := resourceCLI(t, s.Home, "config", "users", ref, "--json")
		var result configUsersResult
		if err != nil || json.Unmarshal([]byte(out), &result) != nil || !result.Complete || !slices.Equal(result.Users, []string{name}) {
			t.Fatal(out, err)
		}
	}
	q.LocalName = "Broken"
	broken, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	brokenDirectory := sessionRecord(t, e, broken.SessionID).Directory
	if err := os.WriteFile(filepath.Join(s.Home, "sessions", brokenDirectory, "session.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = resourceCLI(t, s.Home, "config", "users", "base", "--json")
	var report struct {
		Code    string            `json:"error"`
		Partial configUsersResult `json:"partial_result"`
	}
	if err == nil || json.Unmarshal([]byte(out), &report) != nil || report.Code != "config_usage_unknown" || report.Partial.Complete || !slices.Equal(report.Partial.Users, []string{name}) || !strings.Contains(out, brokenDirectory) {
		t.Fatal("incomplete usage looked authoritative", out, err)
	}
}

func TestDeletionPartialResultsSurviveCLIAndTUIErrors(t *testing.T) {
	for _, mode := range []string{"human", "json", "tui"} {
		t.Run(mode, func(t *testing.T) {
			e, q, first := namedCLIFixture(t)
			q.LocalName = "Second"
			second, err := e.Create(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			e.Docker.Runner.(*dockertest.Daemon).Fail = func(args []string) error {
				if len(args) > 0 && args[0] == "rm" {
					calls++
					if calls == 2 {
						return &docker.ExitError{Code: 23, Operation: "rm"}
					}
				}
				return nil
			}
			var out, stderr bytes.Buffer
			if mode == "tui" {
				input := &choiceScript{t: t, out: &out, steps: []string{"@Delete", "y", "@Back"}}
				cmd := &cobra.Command{Use: "dbx"}
				cmd.SetIn(input)
				cmd.SetOut(&out)
				cmd.SetErr(&stderr)
				cmd.SetContext(context.Background())
				f := frontend{cmd: cmd, m: testMenuCommand(cmd.Context(), input, &out, cmd), e: e, s: &resource.Service{Home: e.Store.Home}}
				if err := f.delete([]string{first, second.SessionID}); err != nil {
					t.Fatal(err)
				}
			} else {
				local := ""
				cmd := deleteCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &local)
				cmd.SilenceErrors, cmd.SilenceUsage = true, true
				cmd.SetIn(strings.NewReader(""))
				cmd.SetOut(&out)
				cmd.SetErr(&stderr)
				args := []string{first, second.SessionID, "--container"}
				if mode == "json" {
					args = append(args, "--json")
				}
				cmd.SetArgs(args)
				if code := Execute(context.Background(), cmd); code != 23 {
					t.Fatal(code, out.String(), stderr.String())
				}
			}
			if mode == "json" {
				var report struct {
					Partial app.DeleteResult `json:"partial_result"`
				}
				decoder := json.NewDecoder(&out)
				if err := decoder.Decode(&report); err != nil || len(report.Partial.Containers) != 1 || len(report.Partial.Retained) != 2 {
					t.Fatal(report, err)
				}
				if err := decoder.Decode(new(any)); err != io.EOF || stderr.Len() != 0 {
					t.Fatal("multiple error payloads", err, stderr.String())
				}
			} else if strings.Count(out.String(), "Deleted container ") != 1 || !strings.Contains(out.String(), "Session state and image retained:") {
				t.Fatal("partial work was hidden", out.String(), stderr.String())
			}
			for _, name := range []string{first, second.SessionID} {
				if _, err := e.Store.Find(context.Background(), name, nil); err != nil {
					t.Fatal("failure removed saved history", err)
				}
			}
		})
	}
}

func TestConfigSaveReceiptPreservesExplicitHome(t *testing.T) {
	cmd := New()
	home := filepath.Join(t.TempDir(), "home with spaces")
	if err := cmd.PersistentFlags().Set("home", home); err != nil {
		t.Fatal(err)
	}
	receipt := configSaveReceipt(cmd, home, "base")
	if !strings.Contains(receipt, "dbx --home "+shellQuote(home)+" status") {
		t.Fatal(receipt)
	}
}

func TestSharedNetworkRenderingAndTailValidation(t *testing.T) {
	for _, tail := range []string{"0", "100", "all"} {
		if err := validateTail(tail); err != nil {
			t.Fatal(err)
		}
	}
	for _, tail := range []string{"-1", "", "many"} {
		if err := validateTail(tail); err == nil {
			t.Fatal(tail)
		}
	}
	values := map[string]string{"B": "two words", "A": "it's"}
	var out bytes.Buffer
	if err := printNetworkEnv(&out, values, ""); err != nil || out.String() != "export A='it'\"'\"'s'\nexport B='two words'\n" {
		t.Fatal(out.String(), err)
	}
	out.Reset()
	if err := printNetworkEnv(&out, values, "B"); err != nil || out.String() != "two words\n" {
		t.Fatal(out.String(), err)
	}
	if err := printNetworkEnv(&out, values, "missing"); err == nil {
		t.Fatal("unknown variable accepted")
	}
}
