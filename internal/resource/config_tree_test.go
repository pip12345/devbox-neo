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

func TestConfigCopiesReportSkippedSymlinks(t *testing.T) {
	for _, operation := range []string{"from-profile", "init-defaults"} {
		t.Run(operation, func(t *testing.T) {
			s := fixture(t)
			ctx := context.Background()
			source, err := s.Profile("source")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Create(ctx, source, ""); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(source.Root, "pi")
			if operation == "init-defaults" {
				h, err := harness.Load(s.Home, "pi")
				if err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(h.Definition)
				if err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(s.Home, "harnesses/pi/harness.json"), string(b))
				root = filepath.Join(s.Home, "harnesses/pi/defaults")
			}
			put(t, filepath.Join(root, "extension.js"), "export {};")
			link := filepath.Join(root, "link")
			if err := os.Symlink("extension.js", link); err != nil {
				t.Fatal(err)
			}
			var result Result
			destination := source
			if operation == "from-profile" {
				destination, err = s.Project(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				result, err = s.Create(ctx, destination, source.Name)
			} else {
				result, err = s.Init(ctx, destination, InitOptions{Harness: "pi", Artifacts: []string{"harness-config"}})
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], link) {
				t.Fatal("copy omitted warning", result)
			}
			if _, err := os.Lstat(filepath.Join(destination.Root, "pi/link")); !os.IsNotExist(err) {
				t.Fatal("symlink copied", err)
			}
			if b := get(t, filepath.Join(destination.Root, "pi/extension.js")); string(b) != "export {};" {
				t.Fatal("regular file was not copied")
			}
		})
	}
}
