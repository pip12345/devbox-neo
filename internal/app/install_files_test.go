package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"devbox/internal/harness"
)

func seedScriptedThird(t *testing.T, e *Engine) string {
	t.Helper()
	seedThird(t, e.Store.Home)
	root := filepath.Join(e.Store.Home, "harnesses", "third")
	var definition harness.Definition
	if err := json.Unmarshal([]byte(thirdDefinition), &definition); err != nil {
		t.Fatal(err)
	}
	definition.Install.Script = "install.sh"
	data, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "harness.json"), string(data))
	write(t, filepath.Join(root, "install", "install.sh"), "true\n")
	write(t, filepath.Join(root, "install", "helper.sh"), "captured\n")
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"third"}`)
	return root
}

func TestImageBuildStagesCapturedHarnessInstallation(t *testing.T) {
	e, d, q := fixture(t)
	root := seedScriptedThird(t, e)
	changed, staged := false, false
	d.Fail = func(args []string) error {
		if args[0] != "build" {
			return nil
		}
		if !changed {
			write(t, filepath.Join(root, "install", "helper.sh"), "changed after resolution\n")
			changed = true
		}
		body, err := os.ReadFile(args[slices.Index(args, "--file")+1])
		if err != nil {
			return err
		}
		file := filepath.Join(args[len(args)-1], "harness-install", "helper.sh")
		if strings.Contains(string(body), "COPY --chown=devuser:devuser") {
			data, err := os.ReadFile(file)
			if err != nil || string(data) != "captured\n" {
				t.Fatal("final image stage did not consume captured install files", err)
			}
			staged = true
		} else if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatal("harness installation leaked into another image stage", err)
		}
		return nil
	}
	if _, err := e.Create(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if !staged {
		t.Fatal("harness installation was not staged")
	}
}

func TestRecreateAdoptsChangedHarnessInstallFiles(t *testing.T) {
	e, d, q := fixture(t)
	root := seedScriptedThird(t, e)
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	forgetSession(t, e, made.SessionID)
	write(t, filepath.Join(root, "install", "helper.sh"), "changed\n")
	if _, err := e.Recreate(context.Background(), recreateRequest(q), false); err != nil {
		t.Fatal("rebuild rejected current install code", err)
	}
	if count(d, "build") != 4 {
		t.Fatal("changed install inputs did not rebuild image")
	}
}
