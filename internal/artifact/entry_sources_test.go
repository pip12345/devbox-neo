package artifact

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/config"
)

func TestEntrySourcesFollowAppendOrderIncludingDuplicates(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	put(t, filepath.Join(home, "config.json"), `{"version":1,"default_profile":"base","default_harness":"pi","global_env":["UNSET","DUP=global"]}`)
	put(t, filepath.Join(home, "profiles/base/config.json"), `{"harness":"pi","version":1,"harness_args":["same","same"],"mounts":["same"],"env":["DUP=profile","EXPR=${env:VALUE}"],"vscode":{"extensions":["same"]},"shell":["bash","-l"]}`)
	put(t, filepath.Join(work, ".devbox/config.json"), `{"harness":"pi","version":1,"harness_args":["same"],"mounts":["same"],"env":["DUP=project"],"vscode":{"extensions":["same"]},"shell":["sh"]}`)
	for _, explicit := range []string{"", "base"} {
		r, err := ResolveWithHost(home, work, explicit, config.Layer{HarnessArgs: []string{"same"}, Env: []string{"DUP=cli"}}, config.Host{"VALUE": "expanded-private-value"})
		if err != nil {
			t.Fatal(err)
		}
		wantArgs := []string{"profile", "profile", "project", "CLI"}
		wantPair := []string{"profile", "project"}
		wantEnv := []string{"global", "profile", "profile", "project", "CLI"}
		wantShell := []string{"project"}
		for key, want := range map[string][]string{"harness_args": wantArgs, "mounts": wantPair, "env": wantEnv, "vscode.extensions": wantPair, "shell": wantShell} {
			if !reflect.DeepEqual(r.Trace.EntrySources[key], want) {
				t.Fatalf("%s sources = %v, want %v", key, r.Trace.EntrySources[key], want)
			}
		}
		if len(r.Trace.EntrySources["env"]) != len(r.Settings.Env) || len(r.Trace.EntrySources["harness_args"]) != len(r.Settings.HarnessArgs) {
			t.Fatal("entry metadata is not aligned with resolved values")
		}
		b, _ := json.Marshal(r.Trace)
		if strings.Contains(string(b), "expanded-private-value") || strings.Contains(string(b), "DUP=") {
			t.Fatal("provenance leaked env values")
		}
	}
}

func TestEntrySourcesRespectStandaloneProjectsAndDefaults(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	put(t, filepath.Join(home, "config.json"), `{"version":1,"default_profile":"base","global_env":["GLOBAL=value"]}`)
	put(t, filepath.Join(home, "profiles/base/config.json"), `{"version":1,"harness":"pi","harness_args":["profile"]}`)
	put(t, filepath.Join(work, ".devbox/config.json"), `{"version":1,"inherit_profile":false,"harness":"pi","harness_args":["project"]}`)
	r, err := Resolve(home, work, "", config.Layer{})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string][]string{"harness_args": {"project"}, "env": {"global"}, "shell": {"built-in default"}} {
		if !reflect.DeepEqual(r.Trace.EntrySources[key], want) {
			t.Fatalf("%s sources = %v, want %v", key, r.Trace.EntrySources[key], want)
		}
	}
}
