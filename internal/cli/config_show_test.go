package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/config"
	"github.com/spf13/cobra"
)

func runSourcesCLI(t *testing.T, e *app.Engine, args ...string) (string, error) {
	t.Helper()
	name := ""
	cmd := editCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	returnOutputErr := cmd.ExecuteContext(context.Background())
	return out.String(), returnOutputErr
}

func TestConfigShowUsesExplicitSourcesAndRedactsEnvironment(t *testing.T) {
	e, q, fullName := namedCLIFixture(t)
	t.Setenv("CONFIG_TEST_SECRET", "never-print-this")
	base := filepath.Join(e.Store.Home, "configs/base/config.json")
	if err := os.WriteFile(base, []byte(`{"harness":"pi","env":["TOKEN=${env:CONFIG_TEST_SECRET}"],"harness_args":["--base"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	overlay := t.TempDir()
	if err := os.WriteFile(filepath.Join(overlay, "config.json"), []byte(`{"harness":"pi","harness_args":["--overlay"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := e.Locate(context.Background(), fullName, "")
	if err != nil {
		t.Fatal(err)
	}
	refs := append(q.Sources, config.Reference{Label: "overlay", Kind: config.ReferenceFixed, Path: overlay})
	if _, err := e.UpdateSources(context.Background(), r, refs); err != nil {
		t.Fatal(err)
	}
	out, err := runSourcesCLI(t, e, q.Workspace, "--name", "Main", "--show", "--json")
	if err != nil || strings.Contains(out, "never-print-this") || !strings.Contains(out, "redacted") || !strings.Contains(out, "--base") || !strings.Contains(out, "--overlay") || !strings.Contains(out, "CONFIG_TEST_SECRET") {
		t.Fatal("unsafe or incomplete combined display", out, err)
	}
	out, err = runSourcesCLI(t, e, fullName, "--show")
	if err != nil || !strings.Contains(strings.Join(strings.Fields(out), " "), "- --base base - --overlay overlay") || strings.Contains(out, "never-print-this") {
		t.Fatal("human display lost entry provenance or redaction", out, err)
	}
}

func TestIncompleteSavedConfigCanBeInspectedWithoutSelectingHarness(t *testing.T) {
	e, _, fullName := namedCLIFixture(t)
	if err := os.WriteFile(filepath.Join(e.Store.Home, "configs/base/config.json"), []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runSourcesCLI(t, e, fullName, "--show", "--json")
	if err != nil || !strings.Contains(out, `"harness":""`) || !strings.Contains(out, "built-in default") {
		t.Fatal(out, err)
	}
	if _, err := e.Open(context.Background(), app.OpenRequest{Target: fullName}); err != nil {
		t.Fatal("incomplete desired config blocked existing runtime", err)
	}
	if _, err := e.Recreate(context.Background(), app.RecreateRequest{Target: fullName}, false); err == nil || !strings.Contains(err.Error(), "No harness selected") {
		t.Fatal("inspection made incomplete configuration applicable", err)
	}
}
