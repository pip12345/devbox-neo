package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"devbox/internal/resource"
)

func TestConfigRowsKeepListItemsBelowAlignedOrigins(t *testing.T) {
	rows := []configDisplayRow{
		{label: "Harness", value: "pi", origin: "profile"},
		{label: "Mounts", value: []string{"/data:/data:ro", "/cache:/cache"}, origin: "default", entryOrigins: []string{"default", "default"}},
		{label: "Environment variables", value: []any{}, origin: "default"},
	}
	var out bytes.Buffer
	if err := printConfigRows(&out, rows, "  ", 80); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(strings.Fields(out.String()), " "), "- /data:/data:ro default - /cache:/cache default") {
		t.Fatal("list entries are not separate indented items", out.String())
	}
	column := -1
	for _, line := range strings.Split(out.String(), "\n") {
		if !strings.HasSuffix(line, "profile") && !strings.HasSuffix(line, "default") {
			continue
		}
		parts := strings.Fields(line)
		if got := strings.LastIndex(line, parts[len(parts)-1]); column >= 0 && got != column {
			t.Fatal("origins shifted with list contents", out.String())
		} else {
			column = got
		}
		if strings.HasPrefix(line, "Mounts:") && strings.Contains(line, "/data") {
			t.Fatal("list was still placed on the heading line")
		}
		if strings.HasPrefix(line, "Environment variables:") && !strings.Contains(line, "None") {
			t.Fatal("empty list did not display as None")
		}
	}
}

func TestConfigWrappingPreservesValues(t *testing.T) {
	text := "start  with spaces/" + strings.Repeat("é/path/", 70) + "  end"
	for _, width := range []int{8, 40, 80} {
		var out bytes.Buffer
		if err := writeConfigLine(&out, "  - ", text, "    ", width); err != nil {
			t.Fatal(err)
		}
		var reconstructed strings.Builder
		for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
			if !utf8.ValidString(line) || utf8.RuneCountInString(line) > width {
				t.Fatal("wrapping split UTF-8 or exceeded the width", line)
			}
			reconstructed.WriteString(line[4:])
		}
		if reconstructed.String() != text {
			t.Fatal("wrapping changed or truncated the value")
		}
	}
	var out bytes.Buffer
	rows := []configDisplayRow{{label: "Scalar", value: strings.Repeat("x", 250), origin: "[profile]"}}
	if err := printConfigRows(&out, rows, "  ", 40); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "x") != 250 {
		t.Fatal("long scalar was truncated")
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if utf8.RuneCountInString(line) > 40 {
			t.Fatal("scalar stretched the source column", line)
		}
	}
}

func TestConfigMenuDisplaysEveryListItemWithoutStretching(t *testing.T) {
	s := menuService(t)
	owner, _ := s.ConfigDirectory("basic", t.TempDir(), t.TempDir())
	s.CreateConfig(context.Background(), owner, resource.SetupOptions{})
	if err := s.SetConfigField(context.Background(), owner, "harness", nil, json.RawMessage(`"pi"`), false); err != nil {
		t.Fatal(err)
	}
	args := make([]string, 12)
	for i := range args {
		args[i] = fmt.Sprintf("--option-%d=value", i)
	}
	long := "--long=" + strings.Repeat("x", 250)
	args = append(args, long)
	value, _ := json.Marshal(args)
	if err := s.SetConfigField(context.Background(), owner, "harness_args", nil, value, false); err != nil {
		t.Fatal(err)
	}
	out, err := runMenu(t, s, owner, "0\n")
	if err != nil {
		t.Fatal(out, err)
	}
	start := strings.Index(out, "Setting")
	if start < 0 {
		t.Fatal("no settings menu")
	}
	menu := out[start:]
	for _, arg := range args[:12] {
		if !strings.Contains(strings.Join(strings.Fields(menu), " "), "• "+arg+" basic") {
			t.Fatal("list item missing from menu", arg, menu)
		}
	}
	if strings.Count(menu, "x") < 250 || strings.Contains(menu, "...") {
		t.Fatal("long list item was truncated", menu)
	}
	for _, line := range strings.Split(menu, "\n") {
		if utf8.RuneCountInString(line) > 80 {
			t.Fatal("list made the overview too wide", line)
		}
	}
}

func TestConfigShowUsesMultilineValuesAndLeavesJSONUnchanged(t *testing.T) {
	e, _, fullName := namedCLIFixture(t)
	home := e.Store.Home
	long := "--long=" + strings.Repeat("x", 250)
	contents := map[string]any{
		"version": 1, "harness": "pi",
		"harness_args": []string{"--first", "--second", long},
		"env":          []string{"TOKEN=do-not-print"},
		"vscode":       map[string]any{"extensions": []string{"example.one", "example.two"}},
	}
	data, _ := json.Marshal(contents)
	if err := os.WriteFile(filepath.Join(home, "configs/base/config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runSourcesCLI(t, e, fullName, "--show")
	if err != nil {
		t.Fatal(out, err)
	}
	for _, text := range []string{"- --first base", "- --second base", "- TOKEN=<redacted> base", "vscode.extensions:", "- example.one base", "- example.two base"} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), text) {
			t.Fatal("missing multiline config detail", text, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if utf8.RuneCountInString(line) > 80 {
			t.Fatal("--show exceeded the readable width", line)
		}
		if strings.HasPrefix(line, "vscode.extensions:") && strings.TrimSpace(line) != "vscode.extensions:" {
			t.Fatal("list heading repeated per-entry source annotations", line)
		}
	}
	if strings.Contains(out, "do-not-print") || strings.Contains(out, "...") || strings.Count(out, "x") < 250 {
		t.Fatal("human display exposed env or truncated a value", out)
	}
	out, err = runSourcesCLI(t, e, fullName, "--show", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var view resource.ConfigView
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal("JSON output contains human formatting", err)
	}
	got, _ := json.Marshal(view.Values["harness_args"])
	want, _ := json.Marshal(contents["harness_args"])
	if !bytes.Equal(got, want) || strings.Contains(out, "do-not-print") {
		t.Fatal("JSON values were reformatted or env was exposed", out)
	}
	if got := strings.Join(view.Trace.EntrySources["harness_args"], ","); got != "base,base,base" {
		t.Fatal("JSON did not retain per-entry provenance", got)
	}
	if _, ok := view.Values["vscode"].(map[string]any); !ok {
		t.Fatal("JSON object was flattened by human display changes")
	}
}

func TestListSelectionWrapsWithoutChangingInputOrLeakingControls(t *testing.T) {
	long := "/data/" + strings.Repeat("long/", 60) + "file"
	var out bytes.Buffer
	m := menu{ctx: context.Background(), in: bufio.NewReader(strings.NewReader("2\n")), out: &out}
	n, err := m.choose("Select entry", []string{configEntryLabel("/data/\x1b[31m", resource.ConfigField{}), long}, "Back")
	if err != nil || n != 1 {
		t.Fatal("wrapping changed selection indices", n, err)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("terminal controls were not escaped")
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if utf8.RuneCountInString(line) > 80 {
			t.Fatal("selection entry was not wrapped")
		}
	}
}
