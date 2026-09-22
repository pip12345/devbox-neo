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

func TestDirectoryMenuShowsOnlyItsOwnContributionsOverDefaults(t *testing.T) {
	s := menuService(t)
	base := testConfigOwner(t, s.Home, "base")
	overlay := testConfigOwner(t, s.Home, "overlay")
	for _, owner := range []resource.Owner{base, overlay} {
		if _, err := s.CreateConfig(context.Background(), owner, resource.SetupOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	put := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put(filepath.Join(s.Home, "config.json"), `{"default_harness":"pi","global_env":["GLOBAL=value"]}`)
	put(filepath.Join(base.Root, "config.json"), `{"harness":"pi","harness_args":["--base"],"env":["BASE=value"],"mounts":["/base:/base"]}`)
	put(filepath.Join(overlay.Root, "config.json"), `{"network":"host","env":["LOCAL=value"],"mounts":["/local:/local"],"vscode":{"extensions":["local.ext"]}}`)
	out, err := runMenu(t, s, overlay, "0\n")
	if err != nil {
		t.Fatal(err)
	}
	for key, suffix := range map[string]string{"harness": "None default", "network": "host overlay", "shell": "bash default", "mounts": "Mounts"} {
		if row := menuSettingRow(t, out, key); !strings.HasSuffix(row, suffix) {
			t.Fatalf("%s: got %q, want %q", key, row, suffix)
		}
	}
	flat := strings.Join(strings.Fields(out), " ")
	for _, expected := range []string{"• /local:/local overlay", "• LOCAL=<redacted> overlay", "• local.ext overlay"} {
		if !strings.Contains(flat, expected) {
			t.Fatal("missing local entry provenance", expected, out)
		}
	}
	for _, unexpected := range []string{"GLOBAL=", "BASE=", "--base", "inherited -", "[11] Inherit"} {
		if strings.Contains(out, unexpected) {
			t.Fatal("directory view imported another source", unexpected, out)
		}
	}
	put(filepath.Join(s.Home, "config.json"), "broken unrelated global config")
	after, err := runMenu(t, s, overlay, "0\n")
	if err != nil || after != out {
		t.Fatal("obsolete global file affected directory editing", err)
	}
}

func TestMenuSourcesDoNotGuessWhenResolutionFails(t *testing.T) {
	s := menuService(t)
	owner := testConfigOwner(t, s.Home, "base")
	s.CreateConfig(context.Background(), owner, resource.SetupOptions{})
	if err := os.WriteFile(filepath.Join(owner.Root, "config.json"), []byte(`{"harness":"pi","network":"invalid network"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runMenu(t, s, owner, "0\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(menuSettingRow(t, out, "harness"), "pi base") || !strings.HasSuffix(menuSettingRow(t, out, "shell"), "Unavailable unknown") {
		t.Fatal("unresolved values were assigned invented sources", out)
	}
}

func TestMenuEmptyHarnessAndNeutralSourceLabels(t *testing.T) {
	s := menuService(t)
	owner := testConfigOwner(t, s.Home, "base")
	s.CreateConfig(context.Background(), owner, resource.SetupOptions{})
	out, err := runMenu(t, s, owner, "0\n")
	if err != nil || !strings.HasSuffix(menuSettingRow(t, out, "harness"), "None default") {
		t.Fatal(out, err)
	}
	if err := os.WriteFile(filepath.Join(owner.Root, "config.json"), []byte(`{"harness":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = runMenu(t, s, owner, "0\n")
	if err != nil || !strings.HasSuffix(menuSettingRow(t, out, "harness"), "None base") {
		t.Fatal("explicit unset lost its provenance", out, err)
	}
	sources := []string{"built-in default", "base"}
	if got := configSourceLabel(sources); got != "default + base" || !reflect.DeepEqual(sources, []string{"built-in default", "base"}) {
		t.Fatal("display mutated source data", sources, got)
	}
}
