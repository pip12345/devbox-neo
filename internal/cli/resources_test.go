package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"devbox/internal/artifact"
	"devbox/internal/commanderror"
	"devbox/internal/config"
	"devbox/internal/resource"
)

func resourceCLI(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	cmd := New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(append([]string{"--home", home}, args...))
	invoked, err := cmd.ExecuteC()
	if err != nil {
		RenderError(invoked, err)
	}
	return out.String(), err
}

func TestFreshConfigSetupNeedsNeitherDockerNorManualJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	for _, args := range [][]string{
		{"config", "create", "basic", "--harness", "pi", "--artifact", "harness-config"},
		{"config", "edit", "basic", "--artifact", "setup.sh"},
	} {
		if out, err := resourceCLI(t, home, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	resolved, err := artifact.Resolve([]config.Source{{Label: "basic", Path: filepath.Join(home, "configs/basic")}}, config.Host{})
	if err != nil || resolved.Settings.Harness != "pi" {
		t.Fatal("new config is not usable", err)
	}
	if _, err := os.Stat(filepath.Join(home, "configs/basic/Dockerfile")); !os.IsNotExist(err) {
		t.Fatal("setup implicitly added a Dockerfile")
	}
	overlay := filepath.Join(t.TempDir(), "overlay")
	if out, err := resourceCLI(t, home, "config", "create", overlay, "--artifact", "harness-config", "--artifact-harness", "opencode"); err != nil {
		t.Fatal(out, err)
	}
	layer, err := config.ReadLayer(filepath.Join(overlay, "config.json"), config.Host{})
	if err != nil || layer.Harness != nil {
		t.Fatal("file generation forced a persistent harness", layer, err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "sessions"))
	if err != nil || len(entries) != 0 {
		t.Fatal("config setup created sessions", entries, err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.json")); !os.IsNotExist(err) {
		t.Fatal("setup seeded a global config")
	}
}

func TestResourceAutomationDoesNotPromptAndScopesNextSteps(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home with spaces")
	out, err := resourceCLI(t, home, "config", "create", "basic", "--json")
	if err != nil || !strings.Contains(out, `"next_steps"`) || !strings.Contains(out, `"--home"`) || strings.Contains(out, "Choose a number") {
		t.Fatal(out, err)
	}
	out, err = resourceCLI(t, home, "config", "create", "basic", "--harness", "pi")
	var exists *commanderror.Error
	if !errors.As(err, &exists) || exists.Code != "config_exists" || !strings.Contains(out, "config edit basic") {
		t.Fatal("repeat creation did not direct to edit", out, err)
	}
	out, err = resourceCLI(t, home, "config", "edit", "missing", "--harness", "pi")
	if err == nil || !strings.Contains(out, "config create missing") || !strings.Contains(out, shellQuote(home)) {
		t.Fatal("missing config has no scoped next step", out, err)
	}
	out, err = resourceCLI(t, home, "config", "edit", "basic", "--json")
	if err == nil || strings.Contains(out, "Choose a number") {
		t.Fatal("JSON edit guessed an operation or prompted", out, err)
	}
}

func TestPromptParsingAndCancellation(t *testing.T) {
	var out bytes.Buffer
	m := menu{ctx: context.Background(), in: bufio.NewReader(strings.NewReader("2\n")), out: &out}
	choice, err := m.selectedChoice("Harness", []string{"pi", "opencode"}, 0, "", "Cancel")
	if err != nil || choice != 1 || !strings.Contains(out.String(), "pi (selected)") || !strings.Contains(out.String(), "Current selection: pi") {
		t.Fatal(choice, err, out.String())
	}
	out.Reset()
	m.in = bufio.NewReader(strings.NewReader("2\n2\n4\n5\n"))
	options, proceed, err := optionalFilesMenu(m, t.TempDir(), resource.SetupOptions{})
	if err != nil || !proceed || !slices.Equal(options.Artifacts, []string{"Dockerfile"}) || strings.Contains(out.String(), "comma") || !strings.Contains(out.String(), "✓ Dockerfile") {
		t.Fatal(options, proceed, err, out.String())
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	reader := terminalReader{ctx: ctx, file: r}
	if _, err = reader.Read(make([]byte, 1)); err != context.DeadlineExceeded {
		t.Fatal("prompt ignored cancellation", err)
	}
}

func TestShellQuoteProtectsCopyableHints(t *testing.T) {
	value := "/work/a'b $(command)"
	if quoted := shellQuote(value); quoted != "'/work/a'\"'\"'b $(command)'" {
		t.Fatal(quoted)
	}
}
