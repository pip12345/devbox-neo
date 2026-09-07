package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/resource"
)

func menuSettingRow(t *testing.T, output, key string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			_, body, _ := strings.Cut(line, "]")
			body = strings.TrimSpace(body)
			if body == configLabel(key) || strings.HasPrefix(body, configLabel(key)+"  ") {
				return strings.Join(strings.Fields(line), " ")
			}
		}
	}
	t.Fatalf("no row for %s:\n%s", key, output)
	return ""
}

func TestMenuSourcesFollowEffectiveLayersRatherThanLocalKeyPresence(t *testing.T) {
	s := menuService(t)
	profile, _ := s.Profile("base")
	project, _ := s.Project(t.TempDir())
	for _, owner := range []resource.Owner{profile, project} {
		if _, err := s.Create(context.Background(), owner, ""); err != nil {
			t.Fatal(err)
		}
	}
	put := func(path, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put(filepath.Join(s.Home, "config.json"), `{"version":1,"default_profile":"base","default_harness":"pi","global_env":["GLOBAL=value"]}`)
	put(filepath.Join(profile.Root, "config.json"), `{"version":1,"on_exit":"running","harness_args":["--base"],"extra_env":["PROFILE=value"],"extra_mounts":["/base:/base"],"vscode":{"extensions":["base.ext"]}}`)
	put(filepath.Join(project.Root, "config.json"), `{"version":1,"network":"host","harness_args":[],"extra_env":["PROJECT=value"],"extra_mounts":["/project:/project"],"vscode":{"extensions":["project.ext"]}}`)
	check := func(want map[string]string) string {
		t.Helper()
		out, err := runMenu(t, s, project, "0\n")
		if err != nil {
			t.Fatal(out, err)
		}
		for key, suffix := range want {
			if row := menuSettingRow(t, out, key); !strings.HasSuffix(row, suffix) {
				t.Fatalf("%s: got %q, want suffix %q", key, row, suffix)
			}
		}
		if strings.Contains(out, "set here") || strings.Contains(out, "(inherited)") || strings.Contains(out, "mixed -") {
			t.Fatal("old source labels remain", out)
		}
		return out
	}
	out := check(map[string]string{
		"harness": "pi inherited - global", "on_exit": "running inherited - profile", "network": "host project",
		"default_shell": "bash default", "harness_args": "Harness arguments", "extra_mounts": "Extra mounts",
		"extra_env": "Environment variables", "vscode": "VS Code extensions", "inherit_profile": "Yes default",
	})
	for _, item := range []string{"• --base inherited - profile", "• /base:/base inherited - profile", "• /project:/project project", "• GLOBAL=<redacted> inherited - global", "• PROFILE=<redacted> inherited - profile", "• PROJECT=<redacted> project", "• base.ext inherited - profile", "• project.ext project"} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), item) {
			t.Fatalf("missing entry provenance %q:\n%s", item, out)
		}
	}
	put(filepath.Join(s.Home, "config.json"), `{"version":1,"default_profile":"base","default_harness":"pi","ignore_project_overrides":true}`)
	check(map[string]string{"network": "default default", "extra_mounts": "Extra mounts", "vscode": "VS Code extensions", "extra_env": "Environment variables"})
	put(filepath.Join(s.Home, "config.json"), `{"version":1,"default_profile":"base","default_harness":"pi"}`)
	put(filepath.Join(project.Root, "config.json"), `{"version":1,"inherit_profile":false,"extra_mounts":["/project:/project"]}`)
	check(map[string]string{"on_exit": "stop default", "extra_mounts": "Extra mounts", "inherit_profile": "No project"})
	put(filepath.Join(project.Root, "config.json"), `{"version":1,"inherit_profile":null}`)
	check(map[string]string{"on_exit": "running inherited - profile", "inherit_profile": "Yes default"})
}

func TestMenuSourcesDoNotGuessWhenResolutionFails(t *testing.T) {
	s := menuService(t)
	owner, _ := s.Profile("base")
	s.Create(context.Background(), owner, "")
	if err := os.WriteFile(filepath.Join(owner.Root, "config.json"), []byte(`{"version":1,"harness":"pi","network":"invalid network"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runMenu(t, s, owner, "0\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(menuSettingRow(t, out, "harness"), "pi profile") || !strings.HasSuffix(menuSettingRow(t, out, "on_exit"), "Unavailable unknown") {
		t.Fatal("unresolved values were assigned invented sources", out)
	}
}

func TestMenuGlobalAndEmptyHarnessSources(t *testing.T) {
	s := menuService(t)
	global, _ := s.ConfigOwner("global", "")
	if err := os.WriteFile(filepath.Join(s.Home, "config.json"), []byte(`{"version":1,"default_harness":"pi"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runMenu(t, s, global, "0\n")
	if err != nil || !strings.HasSuffix(menuSettingRow(t, out, "default_harness"), "pi global") || !strings.HasSuffix(menuSettingRow(t, out, "ignore_project_overrides"), "No default") {
		t.Fatal(out, err)
	}
	profile, _ := s.Profile("base")
	s.Create(context.Background(), profile, "")
	if err := os.WriteFile(filepath.Join(s.Home, "config.json"), []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = runMenu(t, s, profile, "0\n")
	if err != nil || !strings.HasSuffix(menuSettingRow(t, out, "harness"), "None default") {
		t.Fatal(out, err)
	}
	sources := []string{"built-in default", "profile"}
	if got := configSourceLabel(sources); got != "default + profile" || !reflect.DeepEqual(sources, []string{"built-in default", "profile"}) {
		t.Fatal("display mutated or mislabelled source data", sources, got)
	}
}
