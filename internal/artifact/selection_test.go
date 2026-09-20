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
	put(t, filepath.Join(work, ".devbox/config.json"), `{"inherit":false,"harness":"pi"}`)
	if r, err := Resolve(home, work, "work", config.Layer{}); err != nil || r.Profile != "" {
		t.Fatal("inherit cutoff did not discard the explicit profile", r, err)
	}
	if _, err := PreviewSelection(home, work, Selection{Profile: "work", IgnoreProject: true}, config.Layer{}, nil, config.Host{}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectPreviewDoesNotReplaceExcludedSources(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(map[bool]string{false: "invocation", true: "global"}[global], func(t *testing.T) {
			home, work := t.TempDir(), t.TempDir()
			put(t, filepath.Join(home, "profiles/work/config.json"), `{"harness":"pi","ports":["8080:80"]}`)
			if global {
				put(t, filepath.Join(home, "config.json"), `{"ignore_project":true}`)
			}
			proposed, err := config.ParseLayer([]byte(`{"inherit":false,"harness":"opencode"}`))
			if err != nil {
				t.Fatal(err)
			}
			r, err := PreviewSelection(home, work, Selection{Profile: "work", IgnoreProject: !global}, config.Layer{}, &proposed, config.Host{})
			if err != nil || r.Project || r.Settings.Harness != "pi" || !reflect.DeepEqual(r.Settings.Ports, []string{"8080:80"}) {
				t.Fatal("excluded project replaced profile settings", r, err)
			}
		})
	}
}

func TestSourcePreviewStaysBoundAcrossInheritanceCutoffs(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	sources := []config.Source{{Label: "profile", Path: t.TempDir()}, {Label: "project", Path: t.TempDir()}, {Label: "later", Path: t.TempDir()}}
	put(t, filepath.Join(sources[0].Path, "config.json"), "excluded broken config")
	put(t, filepath.Join(sources[1].Path, "config.json"), "replaced by preview")
	put(t, filepath.Join(sources[2].Path, "config.json"), `{"network":"host"}`)
	proposed, err := config.ParseLayer([]byte(`{"inherit":false,"harness":"pi"}`))
	if err != nil {
		t.Fatal(err)
	}
	r, err := PreviewSelection(home, work, Selection{Profile: "work", Sources: sources}, config.Layer{}, &proposed, config.Host{})
	if err != nil || r.Profile != "" || r.Settings.Harness != "pi" || r.Settings.Network != "host" || !reflect.DeepEqual(r.Selection.Sources, sources[1:]) {
		t.Fatal("preview followed source position instead of its directory", r, err)
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
	put(t, filepath.Join(home, "profiles/work/config.json"), `{"harness":"${env:UNSET}"}`)
	put(t, filepath.Join(home, "profiles/fixed/config.json"), `{"harness":"${env:UNSET}"}`)
	p, err := Select(home, work, Selection{}, nil, config.Host{"PROFILE": "work"})
	if err != nil || p.Profile != "work" || !p.Project {
		t.Fatal(p, err)
	}
	pinned := Participation{Profile: "fixed", Project: true, Sources: []config.Source{{Label: "profile", Path: filepath.Join(home, "profiles/fixed")}, {Label: "project", Path: filepath.Join(work, ".devbox")}}}
	p, err = Select(home, work, Selection{Recorded: &pinned}, nil, config.Host{})
	if len(p.Headers) != 2 {
		t.Fatal("missing captured source metadata", p.Headers)
	}
	p.Headers = nil
	if err != nil || !reflect.DeepEqual(p, pinned) {
		t.Fatal(p, err)
	}
}

func TestOldConfigurationFieldsAreRejected(t *testing.T) {
	for _, data := range []string{`{"inherit_profile":false}`, `{"extra_mounts":[]}`, `{"extra_ports":[]}`, `{"extra_env":[]}`, `{"default_shell":["sh"]}`, `{"on_exit":"running"}`} {
		if _, err := config.ParseLayer([]byte(data)); err == nil {
			t.Fatal("accepted removed field", data)
		}
	}
	if _, err := config.ParseGlobal([]byte(`{"ignore_project_overrides":true}`)); err == nil {
		t.Fatal("accepted removed global field")
	}
}
