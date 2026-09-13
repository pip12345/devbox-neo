package filesync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/artifact"
	"devbox/internal/harness"
)

func TestOrdinaryFileOwnership(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "manifest.json")
	desired := map[string]artifact.File{"a.txt": {Data: []byte("first"), Source: "profile", Layer: "profile"}}
	sync := func() error { return Sync(root, manifest, "home", desired, nil) }
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	desired["a.txt"] = artifact.File{Data: []byte("second")}
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "a.txt")
	b, _ := os.ReadFile(path)
	if string(b) != "second" {
		t.Fatal("managed update failed")
	}
	os.WriteFile(path, []byte("user"), 0600)
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if string(b) != "second" {
		t.Fatal("local edit overrode managed source")
	}
	os.WriteFile(path, []byte("user again"), 0600)
	unmanaged := filepath.Join(root, "unmanaged.txt")
	os.WriteFile(unmanaged, []byte("keep"), 0600)
	delete(desired, "a.txt")
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("obsolete managed file survived")
	}
	b, _ = os.ReadFile(unmanaged)
	if string(b) != "keep" {
		t.Fatal("unmanaged file changed")
	}
}
func TestJSONOwnedKeys(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "manifest.json")
	path := filepath.Join(root, "settings.json")
	os.WriteFile(path, []byte(`{"packages":["live"],"theme":"user","nullable":null}`), 0600)
	rules := []harness.Merge{{Path: "settings.json", Strategy: "json-keys", Keys: []string{"packages", "skills"}}}
	desired := map[string]artifact.File{"settings.json": {Data: []byte(`{"packages":["desired"],"theme":"not-owned"}`)}}
	if err := Sync(root, manifest, "home", desired, rules); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var current map[string]any
	json.Unmarshal(b, &current)
	if current["theme"] != "user" || current["packages"].([]any)[0] != "desired" {
		t.Fatal("structured ownership violated")
	}
	if _, ok := current["nullable"]; !ok {
		t.Fatal("lost undeclared null key")
	}
	desired["settings.json"] = artifact.File{Data: []byte(`{}`)}
	if err := Sync(root, manifest, "home", desired, rules); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	current = nil
	json.Unmarshal(b, &current)
	if _, ok := current["packages"]; ok {
		t.Fatal("absent owned key retained")
	}
	os.WriteFile(path, []byte(`[]`), 0600)
	err := Sync(root, manifest, "home", desired, rules)
	var conflicts *Conflicts
	if !errors.As(err, &conflicts) {
		t.Fatalf("want structured conflict: %v", err)
	}
	b, _ = os.ReadFile(path)
	if string(b) != "[]" {
		t.Fatal("invalid live file overwritten")
	}
	desired["settings.json"] = artifact.File{Data: []byte(`invalid`)}
	if err := Sync(root, manifest, "home", desired, rules); err == nil {
		t.Fatal("invalid desired JSON accepted")
	}
}
func TestExecutableConfigAndUserModeChanges(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "manifest.json")
	path := filepath.Join(root, "script.sh")
	desired := map[string]artifact.File{"script.sh": {Data: []byte("#!/bin/sh\nexit 0\n"), Mode: 0755}}
	if err := Sync(root, manifest, "home", desired, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("executable mode not preserved privately")
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err = Sync(root, manifest, "home", desired, nil); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("managed executable mode was not restored", err)
	}
}
func TestSymlinkRejectedWithoutTouchingTarget(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "protected")
	os.WriteFile(outside, []byte("user"), 0600)
	os.Symlink(outside, filepath.Join(root, "link"))
	err := Sync(root, filepath.Join(root, "manifest.json"), "home", map[string]artifact.File{"link": {Data: []byte("overwrite")}}, nil)
	if err == nil {
		t.Fatal("accepted symlink")
	}
	b, _ := os.ReadFile(outside)
	if string(b) != "user" {
		t.Fatal("escaped containment")
	}
}
