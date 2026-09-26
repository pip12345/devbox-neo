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

func TestLocalSourcePickerUsesLaunchDirectoryAndSavesRelativeReference(t *testing.T) {
	launch := t.TempDir()
	workspace := filepath.Join(launch, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	completionFile(t, launch, ".devbox/config.json", `{"version":1}`)
	s := menuService(t)
	completionFile(t, s.Home, "configs/base/config.json", `{"version":1}`)
	before := completionSnapshot(t, launch)
	var out bytes.Buffer
	p := sourcePicker{menu: testMenu(context.Background(), strings.NewReader("1\n"), &out), home: s.Home, workspace: workspace, cwd: launch, userHome: t.TempDir()}
	ref, selected, err := p.choose(nil, "Back")
	if err != nil || !selected || ref.Kind != config.ReferenceRelative || ref.Path != "../.devbox" || ref.Label != "./.devbox" {
		t.Fatal(ref, selected, err, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "Local configs") || !strings.Contains(text, "Named configs") || strings.Index(text, "./.devbox") > strings.Index(text, "base") {
		t.Fatal("local configs were not shown first", text)
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
	before, err := os.ReadFile(filepath.Join(f.s.Home, "sessions", name, "session.json"))
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
	after, err := os.ReadFile(filepath.Join(f.s.Home, "sessions", name, "session.json"))
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
