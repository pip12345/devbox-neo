package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/artifact"
	"devbox/internal/config"
	"devbox/internal/resource"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func menuService(t *testing.T) *resource.Service {
	t.Helper()
	state, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &resource.Service{Home: state.Home}
}

func fieldNumber(t *testing.T, scope, key string) string {
	t.Helper()
	for i, f := range resource.ConfigFields(scope) {
		if f.Key == key {
			return fmt.Sprint(i + 1)
		}
	}
	t.Fatal("missing control", key)
	return ""
}

func runMenu(t *testing.T, s *resource.Service, owner resource.Owner, input string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader(input))
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := runConfigMenu(cmd, s, owner)
	return out.String(), err
}

func TestConfigMenusEditAndResetEachScope(t *testing.T) {
	for _, scope := range []string{"global", "profile", "project"} {
		t.Run(scope, func(t *testing.T) {
			s := menuService(t)
			target := "basic"
			if scope == "project" {
				target = t.TempDir()
			}
			owner, err := s.ConfigOwner(scope, target)
			if err != nil {
				t.Fatal(err)
			}
			if scope != "global" {
				if _, err = s.Create(context.Background(), owner, ""); err != nil {
					t.Fatal(err)
				}
			}
			key, selection, want := "on_exit", "2", `"running"`
			if scope == "global" {
				key, selection, want = "ignore_project_overrides", "1", "true"
			} else if scope == "project" {
				key, selection, want = "inherit_profile", "2", "false"
			}
			n := fieldNumber(t, scope, key)
			out, err := runMenu(t, s, owner, n+"\n1\n"+selection+"\n0\n")
			if err != nil || !strings.Contains(out, "Saved ") || !strings.Contains(out, "(set here)") {
				t.Fatal(out, err)
			}
			source, _ := s.ConfigSource(owner)
			if string(source[key]) != want {
				t.Fatal("wrong menu save", source)
			}
			if out, err = runMenu(t, s, owner, n+"\n2\n0\n"); err != nil {
				t.Fatal(out, err)
			}
			source, _ = s.ConfigSource(owner)
			if _, exists := source[key]; exists {
				t.Fatal("reset saved a default instead of removing the override")
			}
		})
	}
}

func TestConfigMenuPreservesExpressionsAndDoesNotPrintEnvValues(t *testing.T) {
	s := menuService(t)
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	path := filepath.Join(owner.Root, "config.json")
	original := `{"version":1,"extra_env":["TOKEN=private-value","NEXT=${env:UNSET_MENU_REFERENCE}"],"harness":"pi"}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	input := fieldNumber(t, "profile", "network") + "\n1\ndefault\n0\n"
	out, err := runMenu(t, s, owner, input)
	if err != nil || strings.Contains(out, "private-value") || !strings.Contains(out, "redacted") || !strings.Contains(out, "Effective configuration unavailable") {
		t.Fatal(out, err)
	}
	fields, _ := s.ConfigSource(owner)
	var env []string
	if err := json.Unmarshal(fields["extra_env"], &env); err != nil {
		t.Fatal(err)
	}
	if strings.Join(env, ",") != "TOKEN=private-value,NEXT=${env:UNSET_MENU_REFERENCE}" {
		t.Fatal("unrelated edit replaced source env with display values")
	}
	input = fieldNumber(t, "profile", "extra_env") + "\n1\nNEW=another-private-value\n0\n0\n"
	out, err = runMenu(t, s, owner, input)
	if err != nil || strings.Contains(out, "private-value") {
		t.Fatal("env input leaked through list or save feedback", out, err)
	}
	fields, _ = s.ConfigSource(owner)
	if !strings.Contains(string(fields["extra_env"]), "NEW=another-private-value") {
		t.Fatal("env input was not saved literally")
	}
}

func TestMenuEditsOnlyLocalListContribution(t *testing.T) {
	s := menuService(t)
	profile, _ := s.Profile("base")
	s.Create(context.Background(), profile, "")
	os.WriteFile(filepath.Join(profile.Root, "config.json"), []byte(`{"version":1,"harness":"pi","harness_args":["--base"]}`), 0600)
	s.SetDefault(context.Background(), "base")
	project, _ := s.Project(t.TempDir())
	s.Create(context.Background(), project, "")
	n := fieldNumber(t, "project", "harness_args")
	out, err := runMenu(t, s, project, n+"\n1\n--local\n0\n0\n")
	if err != nil {
		t.Fatal(out, err)
	}
	source, _ := s.ConfigSource(project)
	var local []string
	if err := json.Unmarshal(source["harness_args"], &local); err != nil {
		t.Fatal(err)
	}
	if len(local) != 1 || local[0] != "--local" {
		t.Fatal("menu copied inherited entries into local source", source)
	}
	r, err := artifact.Resolve(s.Home, project.Workspace, "", config.Layer{})
	if err != nil || strings.Join(r.Settings.HarnessArgs, ",") != "--base,--local" {
		t.Fatal("menu broke list inheritance", r, err)
	}
}

func TestMenuCancellationAndValidationDoNotWrite(t *testing.T) {
	s := menuService(t)
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	path := filepath.Join(owner.Root, "config.json")
	before, _ := os.ReadFile(path)
	n := fieldNumber(t, "profile", "network")
	for _, input := range []string{"", "0\n", n + "\n0\n0\n", n + "\n1\n:back\n0\n", n + "\n1\n", n + "\n1\nhost", n + "\n1\nnot a network\n0\n"} {
		if out, err := runMenu(t, s, owner, input); err != nil {
			t.Fatal(out, err)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatalf("canceled/invalid edit changed source for %q", input)
		}
	}
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := menu{ctx: ctx, in: bufio.NewReader(strings.NewReader(n + "\n1\nhost\n1\n")), out: &out}
	if err := configMenu(m, s, owner); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

func TestMenuRePromptsAndListEditing(t *testing.T) {
	var out bytes.Buffer
	m := menu{ctx: context.Background(), in: bufio.NewReader(strings.NewReader("bad\n99\n1\n")), out: &out}
	if n, err := m.choose("Pick", []string{"first"}, "Back"); err != nil || n != 0 || !strings.Contains(out.String(), "Choose 1") {
		t.Fatal(out.String(), n, err)
	}
	s := menuService(t)
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	field := resource.ConfigField{Key: "harness_args", Kind: "list"}
	if err := s.SetConfigField(m.ctx, owner, field.Key, nil, json.RawMessage(`["first","second"]`), false); err != nil {
		t.Fatal(err)
	}
	m.in = bufio.NewReader(strings.NewReader("2\n1\nchanged\n3\n2\n0\n"))
	if err := editList(m, s, owner, field); err != nil {
		t.Fatal(err)
	}
	source, _ := s.ConfigSource(owner)
	entries, err := configEntries(source[field.Key], field)
	if err != nil || strings.Join(entries, ",") != "changed" {
		t.Fatal(entries, err)
	}
	m.in = bufio.NewReader(strings.NewReader("3\n1\n0\n"))
	if err := editList(m, s, owner, field); err != nil {
		t.Fatal(err)
	}
	source, _ = s.ConfigSource(owner)
	entries, err = configEntries(source[field.Key], field)
	if err != nil || entries == nil || len(entries) != 0 {
		t.Fatal("removing the last entry did not save an empty list", entries, err)
	}
	m.in = bufio.NewReader(strings.NewReader(""))
	if _, err = m.choose("Pick", []string{"first"}, "Back"); !errors.Is(err, io.EOF) {
		t.Fatal("EOF was treated as a retry", err)
	}
}

func TestConfigCLIRequiresTTYAndShowForJSON(t *testing.T) {
	for _, args := range [][]string{{"global", "config"}, {"global", "config", "--json"}, {"project", "config", ".", "--profile", "basic"}} {
		cmd := New()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader("1\n"))
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--show") {
			t.Fatal("noninteractive config invocation was not rejected", err)
		}
	}
	// The schema-version field remains outside the editable menu.
	if strings.Contains(fieldLabels(resource.ConfigFields("global")), "version") {
		t.Fatal("version exposed as a setting")
	}
}

func fieldLabels(fields []resource.ConfigField) string {
	b, _ := json.Marshal(fields)
	return string(b)
}
