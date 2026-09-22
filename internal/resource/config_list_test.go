package resource

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestListConfigsIncludesBrokenNamedDirectories(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "configs")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"base", "broken", "missing"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "base", "config.json"), []byte(`{"version":1,"harness":"pi"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken", "config.json"), []byte(`{"version":1,"unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "base"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "not-a-config"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	configs, err := (Service{Home: home}).ListConfigs()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range configs {
		names = append(names, entry.Name)
		if entry.Path != filepath.Join(root, entry.Name) {
			t.Fatal("entry lost its selectable path", entry)
		}
		if (entry.Name == "base" || entry.Name == "alias") && (entry.Harness != "pi" || entry.Error != "") {
			t.Fatal("valid config not listed", entry)
		}
		if (entry.Name == "broken" || entry.Name == "missing") && entry.Error == "" {
			t.Fatal("unavailable config hidden", entry)
		}
	}
	if !slices.Equal(names, []string{"alias", "base", "broken", "missing"}) {
		t.Fatal("unexpected entries or order", names)
	}
	if !strings.Contains(configs[3].Error, "config.json") {
		t.Fatal("missing config lacks repair context", configs[3])
	}
}
