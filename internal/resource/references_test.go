package resource

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDirectoryUsageIncludesDescendantsAndAliases(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "bundle")
	child := filepath.Join(root, "child")
	outside := filepath.Join(parent, "bundle-other")
	for _, path := range []string{child, outside} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(child, alias); err != nil {
		t.Fatal(err)
	}
	insideAlias := filepath.Join(root, "external")
	if err := os.Symlink(outside, insideAlias); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		want       bool
	}{
		{"same directory", root, true},
		{"child", child, true},
		{"missing child", filepath.Join(child, "missing"), true},
		{"external alias to child", alias, true},
		{"internal alias to external directory", insideAlias, true},
		{"sibling with matching prefix", outside, false},
		{"parent directory", parent, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := usesConfigDirectory(root, tc.path)
			if err != nil || got != tc.want {
				t.Fatal(got, err)
			}
		})
	}
	broken := filepath.Join(parent, "broken")
	if err := os.Symlink(filepath.Join(parent, "missing"), broken); err != nil {
		t.Fatal(err)
	}
	if _, err := usesConfigDirectory(root, broken); err == nil {
		t.Fatal("unresolved alias was treated as an unused source")
	}
}
