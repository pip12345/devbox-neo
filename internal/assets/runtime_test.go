package assets

import (
	"bytes"
	"path"
	"regexp"
	"strings"
	"testing"
)

func TestRuntimeBundleContainsGuidanceAndLocalDocTargets(t *testing.T) {
	files, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AGENTS.md", "docs/index.md", "docs/reference/configuration.md", "dev/progress.md"} {
		if len(files[name]) == 0 {
			t.Fatal("missing runtime artifact", name)
		}
	}
	if !bytes.Contains(files["AGENTS.md"], []byte("/workspace")) {
		t.Fatal("missing workspace guidance")
	}
	links := regexp.MustCompile(`\]\(([^)]+)\)`)
	for name, data := range files {
		for _, match := range links.FindAllSubmatch(data, -1) {
			target := strings.SplitN(string(match[1]), "#", 2)[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "/") {
				continue
			}
			resolved := path.Clean(path.Join(path.Dir(name), target))
			if _, ok := files[resolved]; !ok {
				t.Fatalf("%s: missing local link %s", name, resolved)
			}
		}
	}
	first, err := Hash()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Hash()
	if err != nil || first != second {
		t.Fatal("unstable runtime fingerprint", err)
	}
}
