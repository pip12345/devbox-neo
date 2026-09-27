package resource

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/harness"
)

func TestArtifactSetupReportsSkippedSymlinks(t *testing.T) {
	s, owner := configDirectoryFixture(t)
	ctx := context.Background()
	if _, err := s.CreateConfig(ctx, owner, SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	h, err := harness.Load(s.Home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(h.Definition)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(s.Home, "harnesses/pi/harness.json"), string(data))
	root := filepath.Join(s.Home, "harnesses/pi/defaults")
	put(t, filepath.Join(root, "extension.js"), "export {};")
	link := filepath.Join(root, "link")
	if err := os.Symlink("extension.js", link); err != nil {
		t.Fatal(err)
	}
	result, err := s.EditConfig(ctx, owner, SetupOptions{ArtifactHarness: "pi", Artifacts: []string{"harness-config"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(owner.Root, "pi/link")); !os.IsNotExist(err) {
		t.Fatal("symlink copied", err)
	}
	if string(get(t, filepath.Join(owner.Root, "pi/extension.js"))) != "export {};" {
		t.Fatal("regular file not copied")
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], link) {
		t.Fatal("missing skipped-entry warning", result.Warnings)
	}
}
