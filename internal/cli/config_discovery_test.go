package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/config"
	"devbox/internal/resource"
)

func TestSourcePickerGroupsAndRelativeSelectionRoots(t *testing.T) {
	launch := t.TempDir()
	workspace := filepath.Join(launch, "workspace")
	for _, root := range []string{launch, workspace} {
		completionFile(t, root, ".devbox/config.json", `{"version":1}`)
		completionFile(t, root, "foreign-app/config.json", `{"theme":"dark"}`)
	}
	s := menuService(t)
	completionFile(t, s.Home, "configs/base/config.json", `{"version":1}`)
	completionFile(t, s.Home, "configs/broken/config.json", `invalid`)
	before := completionSnapshot(t, launch)
	for _, tc := range []struct{ name, input, kind, path, label string }{
		{"named", "1\n", config.ReferenceFixed, filepath.Join(s.Home, "configs/base"), "base"},
		{"workspace", "3\n", config.ReferenceRelative, ".devbox", "./.devbox"},
		{"cwd", "4\n", config.ReferenceRelative, "../.devbox", "./.devbox"},
		{"entered", "5\n./.devbox\n", config.ReferenceRelative, "../.devbox", "./.devbox"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			p := sourcePicker{menu: testMenu(context.Background(), strings.NewReader(tc.input), &out), home: s.Home, workspace: workspace, cwd: launch, userHome: t.TempDir()}
			ref, selected, err := p.choose(nil, "Back")
			if err != nil || !selected || ref.Kind != tc.kind || ref.Path != tc.path || ref.Label != tc.label {
				t.Fatal(ref, selected, err, out.String())
			}
			text := out.String()
			named, work, cwd := strings.Index(text, "Named configs"), strings.Index(text, "Workspace configs"), strings.Index(text, "Current-directory configs")
			if named < 0 || work <= named || cwd <= work {
				t.Fatal("incorrect suggestion group order", text)
			}
			if strings.Contains(text, "(selected)") {
				t.Fatal("discovery marked an unchosen config as selected", text)
			}
			if strings.Contains(text, "foreign-app") || !strings.Contains(text, "broken") {
				t.Fatal("picker must hide invalid local configs but retain invalid named configs", text)
			}
		})
	}
	if !reflect.DeepEqual(before, completionSnapshot(t, launch)) {
		t.Fatal("discovery or selection modified config files")
	}
}

func TestLocalConfigDiscoveryDoesNotSelectOrChangeSession(t *testing.T) {
	launch := t.TempDir()
	t.Chdir(launch)
	completionFile(t, launch, "config.json", `{"version":1,"harness":"pi"}`)
	completionFile(t, launch, ".devbox/config.json", `{"version":1}`)
	f, out, q, name := frontendFixture(t, strings.NewReader("0\n"))
	savedPath := filepath.Join(f.s.Home, "sessions", sessionRecord(t, f.e, name).Directory, "session.json")
	before, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatal(err)
	}
	f.e = nil
	if err := f.browse(true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "./.devbox") || !strings.Contains(out.String(), "base") {
		t.Fatal(out.String())
	}
	out.Reset()
	p, err := newSourcePicker(testMenu(context.Background(), strings.NewReader("0\n"), out), f.s.Home, q.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, selected, err := p.choose(nil, "Back"); err != nil || selected {
		t.Fatal("discovery selected a config", selected, err)
	}
	after, err := os.ReadFile(savedPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("discovery changed saved sources", err)
	}
	named, err := (resource.Service{Home: f.s.Home}).ListConfigs()
	if err != nil || len(named) != 1 || named[0].Name != "base" {
		t.Fatal("discovery registered local configs", named, err)
	}
}

func TestConfigBrowserOpensLocalConfigNotNamedConfig(t *testing.T) {
	launch := t.TempDir()
	t.Chdir(launch)
	completionFile(t, launch, "base/config.json", `{"version":1}`)
	f, out, _, _ := frontendFixture(t, strings.NewReader("1\n0\n0\n"))
	if err := f.browse(true); err != nil {
		t.Fatal(err, out.String())
	}
	if !strings.Contains(out.String(), "Config · ./base") || strings.Contains(out.String(), "Config · base\n") {
		t.Fatal("browser opened the named config instead of the local directory", out.String())
	}
}
