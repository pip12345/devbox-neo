package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedTreesExcludeGitMetadataOnly(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{
		"extensions/repo/.git/config":               "repository metadata",
		"worktree/.git":                             "gitdir: /elsewhere",
		"extensions/repo/main.ts":                   "extension",
		"extensions/repo/node_modules/dep/index.js": "dependency",
		"dist/bundle.js":                            "runtime bundle",
		".settings/options":                         "managed dotfile",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tree, err := ReadTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Files) != 4 || len(tree.Warnings) != 0 {
		t.Fatal("repository metadata captured or runtime content excluded", tree, err)
	}
	for _, name := range []string{"extensions/repo/main.ts", "extensions/repo/node_modules/dep/index.js", "dist/bundle.js", ".settings/options"} {
		if _, ok := tree.Files[name]; !ok {
			t.Fatal("required content excluded", name)
		}
	}
}

func TestInstallationTreeDoesNotInheritManagedGitExclusion(t *testing.T) {
	home := t.TempDir()
	root := scriptHarness(t, home)
	if err := os.MkdirAll(filepath.Join(root, "install/.git"), 0700); err != nil {
		t.Fatal(err)
	}
	authWrite(t, filepath.Join(root, "install/.git/input"), "explicit install input")
	h, err := Load(home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h.InstallFiles[".git/input"]; !ok {
		t.Fatal("managed-content exclusion changed installer inputs")
	}
}
