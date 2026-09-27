package resource

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoverConfigsIsShallowAndIncludesHiddenConfigs(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	for _, path := range []string{parent, root, filepath.Join(root, ".devbox"), filepath.Join(root, "broken"), filepath.Join(root, "nested", "deep")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "config.json"), []byte(`{"version":1,"harness":"pi"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "broken", "config.json"), []byte(`invalid`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, ".devbox"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "file-link"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "config.json"), filepath.Join(root, "file-link", "config.json")); err != nil {
		t.Fatal(err)
	}
	entries, err := DiscoverConfigs(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name)
		if entry.Path != filepath.Join(root, entry.Name) {
			t.Fatal("lost selectable path", entry)
		}
		if entry.Error != "" || entry.Harness != "pi" {
			t.Fatal(entry)
		}
	}
	if !reflect.DeepEqual(names, []string{".", "./.devbox", "./linked"}) {
		t.Fatal("discovery escaped its scope or hid a candidate", names)
	}
}

func TestDiscoverConfigsRequiresValidLayersNotRunnableSettings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		valid   bool
	}{
		{"empty-layer", `{}`, true},
		{"partial-layer", `{"env":["LOG_LEVEL=debug"]}`, true},
		{"host-expression", `{"harness":"${env:DEVBOX_DISCOVERY_HARNESS}"}`, true},
		{"malformed-json", `invalid`, false},
		{"foreign-fields", `{"theme":"dark"}`, false},
		{"wrong-field-type", `{"env":{"LOG_LEVEL":"debug"}}`, false},
		{"duplicate-fields", `{"harness":"pi","harness":"claude"}`, false},
		{"unsupported-version", `{"version":2}`, false},
		{"args-without-harness", `{"harness_args":["--verbose"]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DEVBOX_DISCOVERY_HARNESS", "")
			root := t.TempDir()
			path := filepath.Join(root, "custom-config")
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "config.json"), []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			entries, err := DiscoverConfigs(root)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.valid {
				if len(entries) != 0 {
					t.Fatal("invalid config was offered", entries)
				}
				return
			}
			if len(entries) != 1 || entries[0].Name != "./custom-config" || entries[0].Error != "" {
				t.Fatal("valid layer was not offered", entries)
			}
			if tc.name == "host-expression" && entries[0].Harness != "${env:DEVBOX_DISCOVERY_HARNESS}" {
				t.Fatal("discovery expanded host expressions", entries)
			}
		})
	}
}

func TestDiscoverConfigsDoesNotSeedAnEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	entries, err := DiscoverConfigs(root)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatal("discovery changed the folder", files, err)
	}
	if _, err := DiscoverConfigs(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatal("missing discovery root should be reported", err)
	}
}
