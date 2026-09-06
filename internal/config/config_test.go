package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStrictJSON(t *testing.T) {
	for _, input := range []string{`{"version":1,"proxy":true}`, `{"version":1,}`, `{/*comment*/"version":1}`, `{"version":1,"version":1}`, `{"version":"one"}`, `null`, `{"version":2}`} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadGlobal(path, Host{}); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
}
func TestSparseMerge(t *testing.T) {
	s := Defaults()
	sh := []string{"zsh", "-l"}
	network := "host"
	s.Apply(Layer{Shell: &sh, Network: &network, HarnessArgs: []string{"a"}})
	replace := []string{"bash"}
	s.Apply(Layer{Shell: &replace, HarnessArgs: []string{"b"}})
	if !reflect.DeepEqual(s.Shell, []string{"bash"}) || !reflect.DeepEqual(s.HarnessArgs, []string{"a", "b"}) || s.Network != "host" {
		t.Fatalf("bad merge: %+v", s)
	}
	replace[0] = "mutated"
	if s.Shell[0] != "bash" {
		t.Fatal("shared mutable argv")
	}
}
func TestInheritanceFieldScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"version":1,"inherit_profile":false}`), 0600)
	if _, err := ReadLayer(path, false, Host{}); err == nil {
		t.Fatal("profile accepted inheritance flag")
	}
	if _, err := ReadLayer(path, true, Host{}); err != nil {
		t.Fatal(err)
	}
}
