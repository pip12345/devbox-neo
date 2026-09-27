package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/config"
	"devbox/internal/resource"
)

func TestSelectedSourcesShowLabelsAndPathsWithoutTypes(t *testing.T) {
	s := menuService(t)
	owner := testConfigOwner(t, s.Home, "base")
	if _, err := s.CreateConfig(context.Background(), owner, resource.SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	local := filepath.Join(workspace, "devconfig")
	if err := os.MkdirAll(local, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "config.json"), []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	m := testMenu(context.Background(), strings.NewReader(""), &out)
	sources := []config.Reference{
		{Label: "base", Kind: config.ReferenceFixed, Path: owner.Root},
		{Label: "./devconfig", Kind: config.ReferenceRelative, Path: "devconfig"},
	}
	if err := showSourceChain(m, s.Home, workspace, sources); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Configs, in order:\n   1. base         /") || !strings.Contains(text, "\n   2. ./devconfig  /") || strings.Contains(text, "SOURCE  TYPE") || strings.Contains(text, "base         fixed") || strings.Contains(text, "./devconfig  relative") {
		t.Fatal("selected sources should show only label and expanded path", text)
	}
}

func TestSourcePickerMarksCurrentSelectionUsingEachGroupsDirectory(t *testing.T) {
	s := menuService(t)
	workspace, cwd := t.TempDir(), t.TempDir()
	for _, root := range []string{workspace, cwd} {
		completionFile(t, root, "devconfig/config.json", `{"version":1}`)
	}
	for _, directory := range []string{workspace, cwd} {
		current, err := config.CaptureReference(s.Home, workspace, directory, t.TempDir(), "./devconfig")
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		p := sourcePicker{menu: testMenu(context.Background(), strings.NewReader("0\n"), &out), home: s.Home, workspace: workspace, cwd: cwd, userHome: t.TempDir()}
		if _, chosen, err := p.choose(&current, "Back"); err != nil || chosen {
			t.Fatal(chosen, err)
		}
		if strings.Count(out.String(), "(selected)") != 1 {
			t.Fatal("picker did not mark exactly one current selection", out.String())
		}
		text := out.String()
		first, second := strings.Index(text, "[1]"), strings.Index(text, "[2]")
		if first < 0 || second <= first || strings.Contains(text[first:second], "(selected)") != (directory == workspace) {
			t.Fatal("picker marked the same-named config in the wrong directory", text)
		}
	}
}

func TestSourcePickerShowsReferenceTypeWithoutAnotherConfirmation(t *testing.T) {
	s := menuService(t)
	owner := testConfigOwner(t, s.Home, "base")
	if _, err := s.CreateConfig(context.Background(), owner, resource.SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	local := filepath.Join(workspace, "devconfig")
	if err := os.MkdirAll(local, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "config.json"), []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, input, kind, path string
	}{
		{"named", "1\n", config.ReferenceFixed, owner.Root},
		{"discovered", "2\n", config.ReferenceRelative, "devconfig"},
		{"entered", "3\n./devconfig\n", config.ReferenceRelative, "devconfig"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			p := sourcePicker{
				menu: testMenu(context.Background(), bufio.NewReader(strings.NewReader(test.input)), &out),
				home: s.Home, workspace: workspace, cwd: workspace, userHome: t.TempDir(),
			}
			reference, chosen, err := p.choose(nil, "Back")
			if err != nil || !chosen || reference.Kind != test.kind || reference.Path != test.path {
				t.Fatal("wrong selected reference", reference, chosen, err, out.String())
			}
			text := out.String()
			if !strings.Contains(text, "TYPE      PATH") || !strings.Contains(text, "fixed") || !strings.Contains(text, "relative") || !strings.Contains(text, "\n\n   [3]  Enter a directory path") || strings.Contains(text, "Add this config?") || strings.Contains(text, "Confirm action") {
				t.Fatal("picker did not separate the table or added a confirmation screen", text)
			}
		})
	}
}
