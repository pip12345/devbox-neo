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
	m := menu{out: &out}
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
		{"local", "2\n./devconfig\n", config.ReferenceRelative, "devconfig"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			p := sourcePicker{
				menu: menu{ctx: context.Background(), in: bufio.NewReader(strings.NewReader(test.input)), out: &out},
				home: s.Home, workspace: workspace, cwd: workspace, userHome: t.TempDir(),
			}
			reference, chosen, err := p.choose(nil, "Back")
			if err != nil || !chosen || reference.Kind != test.kind || reference.Path != test.path {
				t.Fatal("wrong selected reference", reference, chosen, err, out.String())
			}
			text := out.String()
			if !strings.Contains(text, "NAME  TYPE   PATH") || !strings.Contains(text, "base  fixed") || !strings.Contains(text, "\n\n   [2]  Enter a directory path") || strings.Contains(text, "Add this config?") || strings.Contains(text, "Confirm") {
				t.Fatal("picker did not separate the table or added a confirmation screen", text)
			}
		})
	}
}
