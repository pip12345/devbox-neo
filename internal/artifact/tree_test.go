package artifact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/config"
	"devbox/internal/harness"
)

func TestSkippedEntriesDoNotOverrideLowerLayers(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	put(t, filepath.Join(home, "config.json"), `{"version":1,"default_profile":"base"}`)
	put(t, filepath.Join(home, "profiles/base/config.json"), `{"version":1,"harness":"pi"}`)
	put(t, filepath.Join(home, "profiles/base/pi/extension.js"), "profile content")
	put(t, filepath.Join(work, ".devbox/config.json"), `{"version":1}`)
	root := filepath.Join(work, ".devbox/pi")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "extension.js")
	if err := os.Symlink("missing", link); err != nil {
		t.Fatal(err)
	}
	h, err := harness.Load(home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"", "base"} {
		sources := []config.Source{{Label: "base", Path: filepath.Join(home, "profiles/base")}}
		if profile == "" {
			sources = append(sources, config.Source{Label: "overlay", Path: filepath.Join(work, ".devbox")})
		}
		r, err := Resolve(sources, config.Snapshot())
		if err != nil {
			t.Fatal(err)
		}
		files, warnings, err := r.Tree(h)
		if err != nil || string(files["extension.js"].Data) != "profile content" {
			t.Fatal("skipped entry changed the layer result", files, err)
		}
		if profile == "" {
			if len(warnings) != 1 || !strings.Contains(warnings[0], link) {
				t.Fatal("missing project warning", warnings)
			}
		} else if len(warnings) != 0 {
			t.Fatal("excluded project generated warnings", warnings)
		}
	}
}
