package artifact

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/config"
	"devbox/internal/harness"
)

func put(t *testing.T, p, b string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(b), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestMissingConfigurationGuidanceIncludesDefaultProfileSelection(t *testing.T) {
	for _, existing := range []bool{false, true} {
		home, work := t.TempDir(), t.TempDir()
		if existing {
			put(t, filepath.Join(home, "profiles/base/config.json"), `{"version":1,"harness":"pi"}`)
		}
		_, err := Resolve(home, work, "", config.Layer{})
		if err == nil {
			t.Fatal("unselected configuration should fail")
		}
		for _, hint := range []string{"devbox-neo profile set <name> (default)", "--profile <name> (this invocation)", "devbox-neo profile create <name>", "devbox-neo project create <folder>"} {
			if !strings.Contains(err.Error(), hint) {
				t.Fatalf("missing guidance %q: %v", hint, err)
			}
		}
	}
}

func TestLayerParticipation(t *testing.T) {
	tests := []struct {
		name, global, profile, project, explicit string
		wantProfile                              string
		wantArgs                                 []string
		fail                                     bool
	}{
		{name: "project wins", global: `{"version":1,"default_profile":"base"}`, profile: `{"version":1,"harness":"pi","harness_args":["profile"]}`, project: `{"version":1,"harness_args":["project"]}`, wantProfile: "base", wantArgs: []string{"profile", "project"}},
		{name: "explicit excludes invalid project", profile: `{"version":1,"harness":"pi","harness_args":["profile"]}`, project: `invalid`, explicit: "base", wantProfile: "base", wantArgs: []string{"profile"}},
		{name: "explicit excludes env references", profile: `{"version":1,"harness":"pi"}`, project: `{"version":1,"network":"${env:MISSING}"}`, explicit: "base", wantProfile: "base"},
		{name: "standalone ignores missing default", global: `{"version":1,"default_profile":"missing"}`, project: `{"version":1,"inherit_profile":false,"harness":"pi"}`},
		{name: "standalone ignores corrupt default", global: `{"version":1,"default_profile":"base"}`, profile: `invalid`, project: `{"version":1,"inherit_profile":false,"harness":"pi"}`},
		{name: "global excludes project inheritance", global: `{"version":1,"default_profile":"base","ignore_project_overrides":true}`, profile: `{"version":1,"harness":"pi"}`, project: `{"version":1,"inherit_profile":false}`, wantProfile: "base"},
		{name: "invalid contributing project fails", global: `{"version":1,"default_profile":"base"}`, profile: `{"version":1,"harness":"pi"}`, project: `invalid`, fail: true},
		{name: "missing selected profile", explicit: "base", fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			work := t.TempDir()
			if tt.global != "" {
				put(t, filepath.Join(home, "config.json"), tt.global)
			}
			if tt.profile != "" {
				put(t, filepath.Join(home, "profiles/base/config.json"), tt.profile)
			}
			if tt.project != "" {
				put(t, filepath.Join(work, ".devbox/config.json"), tt.project)
			}
			r, err := Resolve(home, work, tt.explicit, config.Layer{})
			if (err != nil) != tt.fail {
				t.Fatalf("resolution error: %v", err)
			}
			if tt.fail {
				return
			}
			if r.Profile != tt.wantProfile || !reflect.DeepEqual(r.Settings.HarnessArgs, tt.wantArgs) {
				t.Fatalf("unexpected layers: %+v", r)
			}
		})
	}
}
func TestArtifactsFollowTheSameSelectedLayers(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	put(t, filepath.Join(home, "config.json"), `{"version":1,"default_profile":"base"}`)
	put(t, filepath.Join(home, "profiles/base/config.json"), `{"version":1,"harness":"pi"}`)
	put(t, filepath.Join(work, ".devbox/config.json"), `{"version":1}`)
	put(t, filepath.Join(home, "profiles/base/Dockerfile"), "profile")
	put(t, filepath.Join(work, ".devbox/Dockerfile"), "project")
	put(t, filepath.Join(home, "profiles/base/pi/settings.json"), `{"packages":["profile"]}`)
	put(t, filepath.Join(work, ".devbox/pi/settings.json"), `{"packages":["project"]}`)
	h, err := harness.Load(home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	for _, explicit := range []string{"", "base"} {
		r, err := Resolve(home, work, explicit, config.Layer{})
		if err != nil {
			t.Fatal(err)
		}
		tree, err := r.Tree(h)
		if err != nil {
			t.Fatal(err)
		}
		wanted := "project"
		if explicit != "" {
			wanted = "profile"
		}
		if tree["settings.json"].Layer != wanted {
			t.Fatal("config tree precedence drift")
		}
		b, _ := os.ReadFile(r.Trace.Winners["Dockerfile"])
		if string(b) != wanted {
			t.Fatal("Dockerfile precedence drift")
		}
	}
}
