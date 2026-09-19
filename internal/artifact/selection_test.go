package artifact

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/config"
)

func TestProfileSelectionRetainsProjectAndCanExplicitlyExcludeIt(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	put(t, filepath.Join(home, "profiles/work/config.json"), `{"harness":"pi","mounts":["base:/base"]}`)
	put(t, filepath.Join(work, ".devbox/config.json"), `{"mounts":["project:/project"]}`)
	for _, ignore := range []bool{false, true} {
		r, err := PreviewSelection(home, work, Selection{Profile: "work", IgnoreProject: ignore}, config.Layer{}, nil, config.Host{})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"base:/base"}
		if !ignore {
			want = append(want, "project:/project")
		}
		if r.Profile != "work" || r.Project == ignore || !reflect.DeepEqual(r.Settings.Mounts, want) {
			t.Fatal(r)
		}
	}
	put(t, filepath.Join(work, ".devbox/config.json"), `{"inherit_profile":false,"harness":"pi"}`)
	if _, err := Resolve(home, work, "work", config.Layer{}); err == nil {
		t.Fatal("explicit profile overrode standalone project")
	}
	if _, err := PreviewSelection(home, work, Selection{Profile: "work", IgnoreProject: true}, config.Layer{}, nil, config.Host{}); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessArgumentsRequireAndMatchTheirOwnHarness(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	put(t, filepath.Join(home, "profiles/work/config.json"), `{"harness":"pi","harness_args":["--pi-only"]}`)
	put(t, filepath.Join(work, ".devbox/config.json"), `{"harness":"opencode","harness_args":["--opencode-only"]}`)
	r, err := Resolve(home, work, "work", config.Layer{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Settings.Harness != "opencode" || !reflect.DeepEqual(r.Settings.HarnessArgs, []string{"--opencode-only"}) || !reflect.DeepEqual(r.Trace.EntrySources["harness_args"], []string{"project"}) {
		t.Fatal(r)
	}
	put(t, filepath.Join(work, ".devbox/config.json"), `{"harness":"pi","harness_args":["--project"]}`)
	r, err = Resolve(home, work, "work", config.Layer{})
	if err != nil || !reflect.DeepEqual(r.Settings.HarnessArgs, []string{"--pi-only", "--project"}) {
		t.Fatal(r, err)
	}
	for _, data := range []string{`{"harness_args":["--unspecified"]}`, `{"harness_args":[]}`} {
		put(t, filepath.Join(work, ".devbox/config.json"), data)
		if _, err = Resolve(home, work, "work", config.Layer{}); err == nil || !strings.Contains(err.Error(), "same configuration layer") {
			t.Fatal(err)
		}
	}
}

func TestSelectionDoesNotResolveUnrelatedEnvironmentOrHarnessInputs(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	put(t, filepath.Join(home, "config.json"), `{"default_profile":"${env:PROFILE}","global_env":["SECRET=${env:UNSET}"]}`)
	put(t, filepath.Join(work, ".devbox/config.json"), `{"harness":"${env:UNSET}","mounts":["${env:UNSET}"]}`)
	p, err := Select(home, work, Selection{}, nil, config.Host{"PROFILE": "work"})
	if err != nil || p.Profile != "work" || !p.Project {
		t.Fatal(p, err)
	}
	pinned := Participation{Profile: "fixed", Project: true}
	p, err = Select(home, work, Selection{Recorded: &pinned}, nil, config.Host{})
	if err != nil || p != pinned {
		t.Fatal(p, err)
	}
}

func TestOldConfigurationFieldsAreRejected(t *testing.T) {
	for _, data := range []string{`{"extra_mounts":[]}`, `{"extra_ports":[]}`, `{"extra_env":[]}`, `{"default_shell":["sh"]}`, `{"on_exit":"running"}`} {
		if _, err := config.ParseLayer([]byte(data), true); err == nil {
			t.Fatal("accepted removed field", data)
		}
	}
	if _, err := config.ParseGlobal([]byte(`{"ignore_project_overrides":true}`)); err == nil {
		t.Fatal("accepted removed global field")
	}
}
