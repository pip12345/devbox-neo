package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/harness"
)

func TestOpenWarnsAndSkipsConfigSymlinks(t *testing.T) {
	for _, source := range []string{"profile", "project", "defaults"} {
		t.Run(source, func(t *testing.T) {
			e, _, q := fixture(t)
			root := filepath.Join(e.Store.Home, "profiles/test/pi")
			switch source {
			case "project":
				q.Profile = ""
				root = filepath.Join(q.Workspace, ".devbox/pi")
				write(t, filepath.Join(q.Workspace, ".devbox/config.json"), `{"version":1,"harness":"pi"}`)
			case "defaults":
				h, err := harness.Load(e.Store.Home, "pi")
				if err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(h.Definition)
				if err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(e.Store.Home, "harnesses/pi/harness.json"), string(b))
				root = filepath.Join(e.Store.Home, "harnesses/pi/defaults")
			}
			name := "extensions-pip/node_modules/.bin/anthropic-ai-sdk"
			link := filepath.Join(root, name)
			write(t, filepath.Join(root, "extensions-pip/index.js"), "export {};")
			if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../sdk/cli.js", link); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Create(context.Background(), q); err != nil {
				t.Fatal(err)
			}
			e.Streams.Err = new(bytes.Buffer)
			for i := 0; i < 2; i++ {
				result, err := e.Open(context.Background(), q)
				if err != nil {
					t.Fatal(err)
				}
				live := filepath.Join(e.Store.Home, "sessions", result.Name, "harnesses/pi/stores/home")
				if _, err := os.Lstat(filepath.Join(live, name)); !os.IsNotExist(err) {
					t.Fatal("symlink should not be copied", err)
				}
				if b, err := os.ReadFile(filepath.Join(live, "extensions-pip/index.js")); err != nil || string(b) != "export {};" {
					t.Fatal("regular config was not synchronized", err)
				}
			}
			warning := fmt.Sprintf("Warning: skipping non-regular config entry %q", link)
			if output := fmt.Sprint(e.Streams.Err); strings.Count(output, warning) != 2 {
				t.Fatal("each open must report the skipped source path", output)
			}
		})
	}
}
