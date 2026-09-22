package artifact

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/config"
)

func TestSourcePreviewStaysBoundToItsDirectory(t *testing.T) {
	base := testSource(t, "base", `{"harness":"opencode","ports":["8080:80"]}`)
	selected := testSource(t, "selected", "replaced by preview")
	last := testSource(t, "last", `{"network":"host"}`)
	proposed, err := config.ParseLayer([]byte(`{"harness":"pi","network":"default"}`))
	if err != nil {
		t.Fatal(err)
	}
	preview := &SourcePreview{Path: selected.Path, Layer: proposed}
	for _, sources := range [][]config.Source{{base, last, selected}, {last, base, selected}} {
		r, err := Preview(sources, preview, config.Host{})
		if err != nil || r.Settings.Harness != "pi" || r.Settings.Network != "default" || !reflect.DeepEqual(r.Sources, sources) || len(r.Settings.Ports) != 1 {
			t.Fatal("preview changed participation or followed an array position", r, err)
		}
	}
	if _, err := Preview([]config.Source{base}, preview, config.Host{}); err != nil {
		t.Fatal("an unselected preview changed composition", err)
	}
}

func TestHarnessArgumentsRequireAndMatchTheirOwnHarness(t *testing.T) {
	base := testSource(t, "base", `{"harness":"pi","harness_args":["--pi-only"]}`)
	overlay := testSource(t, "overlay", `{"harness":"opencode","harness_args":["--opencode-only"]}`)
	sources := []config.Source{base, overlay}
	r, err := Resolve(sources, config.Host{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Settings.Harness != "opencode" || !reflect.DeepEqual(r.Settings.HarnessArgs, []string{"--opencode-only"}) || !reflect.DeepEqual(r.Trace.EntrySources["harness_args"], []string{"overlay"}) {
		t.Fatal(r)
	}
	put(t, filepath.Join(overlay.Path, "config.json"), `{"harness":"pi","harness_args":["--overlay"]}`)
	r, err = Resolve(sources, config.Host{})
	if err != nil || !reflect.DeepEqual(r.Settings.HarnessArgs, []string{"--pi-only", "--overlay"}) {
		t.Fatal(r, err)
	}
	for _, data := range []string{`{"harness_args":["--unspecified"]}`, `{"harness_args":[]}`} {
		put(t, filepath.Join(overlay.Path, "config.json"), data)
		if _, err = Resolve(sources, config.Host{}); err == nil || !strings.Contains(err.Error(), "same configuration layer") {
			t.Fatal(err)
		}
	}
}

func TestOldConfigurationFieldsAreRejected(t *testing.T) {
	for _, data := range []string{`{"inherit":false}`, `{"inherit_profile":false}`, `{"extra_mounts":[]}`, `{"extra_ports":[]}`, `{"extra_env":[]}`, `{"default_shell":["sh"]}`, `{"on_exit":"running"}`, `{"ignore_project_overrides":true}`, `{"ignore_project":true}`} {
		if _, err := config.ParseLayer([]byte(data)); err == nil {
			t.Fatal("accepted removed field", data)
		}
	}
}
