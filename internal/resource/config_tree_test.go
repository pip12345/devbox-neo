package resource

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/artifact"
	"devbox/internal/harness"
)

func TestSourceTreesAndArtifactSetupReportSkippedSymlinks(t *testing.T) {
	for _, operation := range []string{"source-tree", "seed-defaults"} {
		t.Run(operation, func(t *testing.T) {
			s, owner := configDirectoryFixture(t)
			ctx := context.Background()
			if _, err := s.CreateConfig(ctx, owner, SetupOptions{}); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(owner.Root, "pi")
			if operation == "seed-defaults" {
				h, err := harness.Load(s.Home, "pi")
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(h.Definition)
				if err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(s.Home, "harnesses/pi/harness.json"), string(data))
				root = filepath.Join(s.Home, "harnesses/pi/defaults")
			}
			put(t, filepath.Join(root, "extension.js"), "export {};")
			link := filepath.Join(root, "link")
			if err := os.Symlink("extension.js", link); err != nil {
				t.Fatal(err)
			}
			var warnings []string
			if operation == "source-tree" {
				tree, err := artifact.SourceTree(owner.Root, map[string]bool{"pi": true})
				if err != nil {
					t.Fatal(err)
				}
				warnings = tree.Warnings
				if _, exists := tree.Files["pi/link"]; exists || string(tree.Files["pi/extension.js"].Data) != "export {};" {
					t.Fatal("source tree included a link or lost a regular file")
				}
			} else {
				result, err := s.EditConfig(ctx, owner, SetupOptions{ArtifactHarness: "pi", Artifacts: []string{"harness-config"}})
				if err != nil {
					t.Fatal(err)
				}
				warnings = result.Warnings
				if _, err := os.Lstat(filepath.Join(owner.Root, "pi/link")); !os.IsNotExist(err) {
					t.Fatal("symlink copied", err)
				}
				if string(get(t, filepath.Join(owner.Root, "pi/extension.js"))) != "export {};" {
					t.Fatal("regular file not copied")
				}
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0], link) {
				t.Fatal("missing skipped-entry warning", warnings)
			}
		})
	}
}
