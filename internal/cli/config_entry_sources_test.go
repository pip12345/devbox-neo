package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProfileOverviewShowsGlobalEntriesButEditorDoesNot(t *testing.T) {
	s := menuService(t)
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	for path, contents := range map[string]string{
		filepath.Join(s.Home, "config.json"):     `{"version":1,"global_env":["GLOBAL=global-private-value"]}`,
		filepath.Join(owner.Root, "config.json"): `{"version":1,"extra_env":["PROFILE=profile-private-value"]}`,
	} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := runMenu(t, s, owner, fieldNumber(t, "profile", "extra_env")+"\n0\n0\n")
	if err != nil {
		t.Fatal(out, err)
	}
	flat := strings.Join(strings.Fields(out), " ")
	for _, expected := range []string{"• GLOBAL=<redacted> inherited - global", "• PROFILE=<redacted> profile"} {
		if !strings.Contains(flat, expected) {
			t.Fatal("overview lost an entry's origin", expected, out)
		}
	}
	start := strings.Index(out, "Environment variables (extra_env)")
	if start < 0 {
		t.Fatal("missing editor", out)
	}
	end := strings.Index(out[start:], "Profile · basic")
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
	global, err := s.ShowGlobal()
	if err != nil || !reflect.DeepEqual(global.Trace.EntrySources["global_env"], []string{"global"}) {
		t.Fatal("global view lost entry provenance", global.Trace, err)
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
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	data, _ := json.Marshal(args)
	if err := s.SetConfigField(context.Background(), owner, "default_shell", nil, data, false); err != nil {
		t.Fatal(err)
	}
	out, err := runMenu(t, s, owner, "0\n")
	if err != nil || !strings.Contains(menuSettingRow(t, out, "default_shell"), "bash -lc 'echo hello' profile") {
		t.Fatal(out, err)
	}
	view, err := s.ShowProfile(owner.Name)
	if err != nil {
		t.Fatal(err)
	}
	value, _ := json.Marshal(view.Values["default_shell"])
	if string(value) != string(data) {
		t.Fatal("display changed JSON shell argv")
	}
	if got := configEntryOrigins("project", []string{"global", "profile", "project", "built-in default"}); !reflect.DeepEqual(got, []string{"inherited - global", "inherited - profile", "project", "default"}) {
		t.Fatal("wrong scope-relative labels", got)
	}
}
