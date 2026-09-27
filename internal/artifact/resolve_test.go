package artifact

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"devbox/internal/config"
	"devbox/internal/harness"
)

func put(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func testSource(t *testing.T, label, data string) config.Source {
	t.Helper()
	source := config.Source{Label: label, Path: t.TempDir()}
	put(t, filepath.Join(source.Path, "config.json"), data)
	return source
}

func TestOnlyExplicitSourcesContribute(t *testing.T) {
	first := testSource(t, "base", `{"harness":"pi","harness_args":["base"]}`)
	second := testSource(t, "overlay", `{"harness":"pi","harness_args":["overlay"]}`)
	for _, sources := range [][]config.Source{{first}, {first, second}, {second, first}} {
		resolved, err := Resolve(sources, config.Host{})
		if err != nil {
			t.Fatal(err)
		}
		var expected []string
		for _, source := range sources {
			expected = append(expected, source.Label)
		}
		if !reflect.DeepEqual(resolved.Settings.HarnessArgs, expected) || !reflect.DeepEqual(resolved.Sources, sources) {
			t.Fatal("source order changed", resolved)
		}
	}
	put(t, filepath.Join(first.Path, "config.json"), "broken")
	if _, err := Resolve([]config.Source{first, second}, config.Host{}); err == nil {
		t.Fatal("later config bypassed a broken source")
	}
	if _, err := Resolve([]config.Source{second}, config.Host{}); err != nil {
		t.Fatal("unselected source was read", err)
	}
	if err := os.Remove(filepath.Join(first.Path, "config.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve([]config.Source{first}, config.Host{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lost the missing-file cause", err)
	}
}

func TestArtifactsFollowTheSameSelectedSources(t *testing.T) {
	first := testSource(t, "base", `{"harness":"pi"}`)
	second := testSource(t, "overlay", `{}`)
	for _, source := range []config.Source{first, second} {
		put(t, filepath.Join(source.Path, Dockerfile), source.Label)
		put(t, filepath.Join(source.Path, "pi/settings.json"), `{"packages":["`+source.Label+`"]}`)
	}
	h, err := harness.Load(t.TempDir(), "pi")
	if err != nil {
		t.Fatal(err)
	}
	for _, sources := range [][]config.Source{{first, second}, {second, first}} {
		r, err := Resolve(sources, config.Host{})
		if err != nil {
			t.Fatal(err)
		}
		tree, _, err := r.Tree(h)
		if err != nil {
			t.Fatal(err)
		}
		last := sources[len(sources)-1]
		if tree["settings.json"].Layer != last.Label || len(r.Trace.Artifacts[Dockerfile]) != 2 {
			t.Fatal("artifact composition drifted from source order")
		}
		data, _ := os.ReadFile(r.Trace.Artifacts[Dockerfile][1])
		if string(data) != last.Label {
			t.Fatal("Dockerfile order changed")
		}
	}
}
