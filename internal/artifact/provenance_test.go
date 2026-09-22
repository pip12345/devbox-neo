package artifact

import (
	"path/filepath"
	"reflect"
	"testing"

	"devbox/internal/config"
)

func TestSourcesTrackExplicitEnvironmentAndSelectedHarness(t *testing.T) {
	base := testSource(t, "base", `{"harness":"pi","env":["BASE=value"]}`)
	overlay := testSource(t, "overlay", `{"env":["OVERLAY=value"]}`)
	sources := []config.Source{base, overlay}
	r, err := Resolve(sources, config.Host{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Trace.Sources["env"], []string{"base", "overlay"}) || !reflect.DeepEqual(r.Trace.Sources["harness"], []string{"base"}) {
		t.Fatal("lost source contributions", r.Trace.Sources)
	}
	put(t, filepath.Join(overlay.Path, "config.json"), `{"harness":""}`)
	r, err = Resolve(sources, config.Host{})
	if err != nil || r.Settings.Harness != "" || !reflect.DeepEqual(r.Trace.Sources["harness"], []string{"overlay"}) {
		t.Fatal("explicit empty selection was attributed to another source", r, err)
	}
}
