package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigShowUsesRuntimeLayersAndRedactsEnvironment(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("CONFIG_TEST_SECRET", "never-print-this")
	resourceCLI(t, home, "profile", "create", "base")
	p := filepath.Join(home, "profiles/base/config.json")
	os.WriteFile(p, []byte(`{"version":1,"harness":"pi","extra_env":["TOKEN=${env:CONFIG_TEST_SECRET}"],"harness_args":["--base"]}`), 0600)
	resourceCLI(t, home, "profile", "set", "base")
	resourceCLI(t, home, "project", "create", workspace)
	os.WriteFile(filepath.Join(workspace, ".devbox/config.json"), []byte(`{"version":1,"harness_args":["--project"]}`), 0600)
	out, err := resourceCLI(t, home, "project", "config", workspace, "--show", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "never-print-this") || !strings.Contains(out, "redacted") || !strings.Contains(out, "--base") || !strings.Contains(out, "--project") || !strings.Contains(out, "CONFIG_TEST_SECRET") {
		t.Fatal("incorrect/redaction-unsafe resolved display", out)
	}
	out, err = resourceCLI(t, home, "project", "config", workspace, "--show")
	if err != nil || !strings.Contains(out, "\n  - --base\n  - --project\n") || strings.Contains(out, "never-print-this") {
		t.Fatal("human display lost list entries or redaction", out, err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "harness_args:") && !strings.Contains(line, "[profile -> project]") {
			t.Fatal("human display lost contribution sources", line)
		}
	}
	os.WriteFile(filepath.Join(workspace, ".devbox/config.json"), []byte(`{"harness":"${env:UNSET_EXCLUDED}"}`), 0600)
	out, err = resourceCLI(t, home, "project", "config", workspace, "--profile", "base", "--show", "--json")
	if err != nil || !strings.Contains(out, `"excluded":["project"]`) {
		t.Fatal("display did not use explicit-profile isolation", out, err)
	}
}
func TestSparseConfigCanBeShownBeforeHarnessSelection(t *testing.T) {
	home := t.TempDir()
	resourceCLI(t, home, "profile", "create", "empty")
	if out, err := resourceCLI(t, home, "profile", "config", "empty", "--show"); err != nil || !strings.Contains(out, "built-in default") {
		t.Fatal(out, err)
	}
	if out, err := resourceCLI(t, home, "global", "config", "--show", "--json"); err != nil || !strings.Contains(out, `"default_profile":""`) {
		t.Fatal(out, err)
	}
}
