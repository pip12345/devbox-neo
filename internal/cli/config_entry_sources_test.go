package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/resource"
)

func TestDirectoryOverviewAndEditorExcludeObsoleteGlobalEntries(t *testing.T) {
	s := menuService(t)
	owner, _ := s.ConfigDirectory("basic", t.TempDir(), t.TempDir())
	s.CreateConfig(context.Background(), owner, resource.SetupOptions{})
	for path, contents := range map[string]string{
		filepath.Join(s.Home, "config.json"):     `{"version":1,"global_env":["GLOBAL=global-private-value"]}`,
		filepath.Join(owner.Root, "config.json"): `{"version":1,"env":["PROFILE=profile-private-value"]}`,
	} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := runMenu(t, s, owner, fieldNumber(t, "env")+"\n0\n0\n")
	if err != nil {
		t.Fatal(out, err)
	}
	flat := strings.Join(strings.Fields(out), " ")
	for _, expected := range []string{"• PROFILE=<redacted> basic"} {
		if !strings.Contains(flat, expected) {
			t.Fatal("overview lost an entry's origin", expected, out)
		}
	}
	start := strings.Index(out, "Environment variables (env)")
	if start < 0 {
		t.Fatal("missing editor", out)
	}
	end := strings.Index(out[start:], "Config · basic")
	if end < 0 {
		t.Fatal("missing editor or return to overview", out)
	}
	editor := out[start : start+end]
	if strings.Contains(editor, "GLOBAL=") || !strings.Contains(editor, "PROFILE=<redacted>") {
		t.Fatal("editor exposed inherited entries for editing", editor)
	}
	if strings.Contains(out, "private-value") {
		t.Fatal("display leaked environment values")
	}
	view, err := s.ShowOwner(owner)
	if err != nil || !reflect.DeepEqual(view.Trace.EntrySources["env"], []string{"basic"}) || strings.Contains(out, "GLOBAL=") {
		t.Fatal("directory view imported global entries or lost provenance", view.Trace, err)
	}
}

func TestShellDisplayIsACommandWithoutChangingStoredArgv(t *testing.T) {
	args := []string{"bash", "-lc", "echo hello"}
	row := configDisplayRow{value: args, command: true}
	text, items := row.parts()
	if text != "bash -lc 'echo hello'" || len(items) != 0 {
		t.Fatal("shell was not rendered as a command", text, items)
	}
	if !reflect.DeepEqual(args, []string{"bash", "-lc", "echo hello"}) {
		t.Fatal("formatting changed argv")
	}
	s := menuService(t)
	owner, _ := s.ConfigDirectory("basic", t.TempDir(), t.TempDir())
	s.CreateConfig(context.Background(), owner, resource.SetupOptions{})
	data, _ := json.Marshal(args)
	if err := s.SetConfigField(context.Background(), owner, "shell", nil, data, false); err != nil {
		t.Fatal(err)
	}
	out, err := runMenu(t, s, owner, "0\n")
	if err != nil || !strings.Contains(menuSettingRow(t, out, "shell"), "bash -lc 'echo hello' basic") {
		t.Fatal(out, err)
	}
	view, err := s.ShowOwner(testConfigOwner(t, s.Home, owner.Name))
	if err != nil {
		t.Fatal(err)
	}
	value, _ := json.Marshal(view.Values["shell"])
	if string(value) != string(data) {
		t.Fatal("display changed JSON shell argv")
	}
	if got := configEntryOrigins([]string{"global", "profile", "project", "built-in default"}); !reflect.DeepEqual(got, []string{"global", "profile", "project", "default"}) {
		t.Fatal("wrong scope-relative labels", got)
	}
}
