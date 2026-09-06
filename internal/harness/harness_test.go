package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestBuiltinAndUserOverrideUseSameSchema(t *testing.T) {
	home := t.TempDir()
	builtin, err := Load(home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if builtin.Origin != "builtin" || len(builtin.Definition.Merge) != 1 || builtin.Definition.Merge[0].Strategy != "json-keys" {
		t.Fatal("Pi did not use parsed declarations")
	}
	custom := builtin.Definition
	custom.Binary = "custom-pi"
	custom.Stores[0].Target = "/home/${user}/.custom"
	p := filepath.Join(home, "harnesses/pi/harness.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	b, _ := json.Marshal(custom)
	os.WriteFile(p, b, 0600)
	effective, err := Load(home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if effective.Origin != p || effective.Definition.Binary != "custom-pi" || effective.Definition.Stores[0].Target != "/home/devuser/.custom" || effective.Hash == builtin.Hash {
		t.Fatal("user override/template was not applied")
	}
	if len(effective.Defaults) != 0 {
		t.Fatal("user override inherited builtin defaults")
	}
	os.WriteFile(p, []byte(`invalid`), 0600)
	if _, err = Load(home, "pi"); err == nil {
		t.Fatal("invalid override fell back to builtin")
	}
}
func TestUnsafeAndOverlappingDeclarations(t *testing.T) {
	for _, mutate := range []func(*Definition){
		func(d *Definition) { d.Stores[0].Target = "/" },
		func(d *Definition) { d.Stores[1].Target = d.Stores[0].Target + "/nested" },
		func(d *Definition) { d.Config.Path = "../escape" },
		func(d *Definition) { d.Auth[0].Source = "../escape" },
		func(d *Definition) { d.Merge[0].Keys = []string{"same", "same"} },
		func(d *Definition) { d.Merge = append(d.Merge, d.Merge[0]) },
	} {
		h, err := Load(t.TempDir(), "pi")
		if err != nil {
			t.Fatal(err)
		}
		mutate(&h.Definition)
		if err = h.Definition.Validate(); err == nil {
			t.Fatal("unsafe declaration accepted")
		}
	}
}
func TestRegistryReportsBrokenOverridesWithoutHidingValidChoices(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "harnesses/pi/harness.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte("bad"), 0600)
	h, err := Load(home, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	if h.Definition.Config.Store != "config" || len(h.Definition.Stores) != 3 || len(h.Definition.Merge) != 0 {
		t.Fatal("unexpected OpenCode declarations")
	}
	custom := h.Definition
	custom.Name = "third"
	b, _ := json.Marshal(custom)
	p = filepath.Join(home, "harnesses/third/harness.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, b, 0600)
	registry, err := Enumerate(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Valid) != 2 || registry.Valid[0].Definition.Name != "opencode" || registry.Valid[1].Definition.Name != "third" || len(registry.Invalid) != 1 || registry.Invalid[0].Name != "pi" {
		t.Fatal("registry hid an invalid override or valid choice")
	}
}
func TestEmbeddedAndHostDefaultsShareRecursiveFileRules(t *testing.T) {
	tree, err := readTree(fstest.MapFS{
		"nested/file": {Data: []byte("value"), Mode: 0700},
		"link":        {Mode: os.ModeSymlink},
		"pipe":        {Mode: os.ModeNamedPipe},
	}, "defaults")
	if err != nil || len(tree.Files) != 1 || string(tree.Files["nested/file"].Data) != "value" || tree.Files["nested/file"].Mode != 0700 {
		t.Fatal(tree, err)
	}
	if len(tree.Warnings) != 2 {
		t.Fatal("unsupported entries must each produce a warning", tree.Warnings)
	}
}
func TestCanonicalPathsAndAuthCannotObscureStores(t *testing.T) {
	for _, change := range []func(*Definition){func(d *Definition) { d.Config.Path = "a/../b" }, func(d *Definition) { d.Auth[0].Target = "/home/devuser/.pi"; d.Auth[0].Kind = "directory" }} {
		h, err := Load(t.TempDir(), "pi")
		if err != nil {
			t.Fatal(err)
		}
		change(&h.Definition)
		if err = h.Definition.Validate(); err == nil {
			t.Fatal("ambiguous path or obscured store accepted")
		}
	}
}
func TestInvalidUnselectedDefinitionDoesNotBlockSelection(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "harnesses/broken/harness.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte("bad"), 0600)
	if _, err := Load(home, "pi"); err != nil {
		t.Fatal(err)
	}
}
