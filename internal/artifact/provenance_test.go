package artifact

import (
	"path/filepath"
	"reflect"
	"testing"

	"devbox/internal/config"
)

func TestSourcesTrackGlobalEnvironmentAndSelectedHarness(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	put(t, filepath.Join(home, "config.json"), `{"version":1,"default_profile":"base","default_harness":"pi","global_env":["GLOBAL=value"]}`)
	put(t, filepath.Join(home, "profiles/base/config.json"), `{"version":1,"env":["PROFILE=value"]}`)
	put(t, filepath.Join(work, ".devbox/config.json"), `{"version":1,"env":["PROJECT=value"]}`)
	r, err := Resolve(home, work, "", config.Layer{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Trace.Sources["env"], []string{"global", "profile", "project"}) {
		t.Fatal("global env contribution is missing", r.Trace.Sources)
	}
	if !reflect.DeepEqual(r.Trace.Sources["harness"], []string{"global"}) {
		t.Fatal("selected global harness source is missing", r.Trace.Sources)
	}
	put(t, filepath.Join(home, "config.json"), `{"version":1,"default_profile":"base"}`)
	put(t, filepath.Join(home, "profiles/base/config.json"), `{"version":1,"harness":""}`)
	r, err = Resolve(home, work, "", config.Layer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Trace.Sources["harness"]) != 0 || r.Settings.Harness != "" {
		t.Fatal("no harness selected should not claim a global or profile source", r.Trace.Sources)
	}
	if !reflect.DeepEqual(r.Trace.Sources["env"], []string{"project"}) {
		t.Fatal("absent global env was counted as a contribution", r.Trace.Sources)
	}
}
