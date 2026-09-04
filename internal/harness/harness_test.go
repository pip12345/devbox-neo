package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
func TestInvalidUnselectedDefinitionDoesNotBlockSelection(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "harnesses/broken/harness.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte("bad"), 0600)
	if _, err := Load(home, "pi"); err != nil {
		t.Fatal(err)
	}
}
