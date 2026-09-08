package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/artifact"
	"devbox/internal/config"
)

func resourceCLI(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	cmd := New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(append([]string{"--home", home}, args...))
	err := cmd.Execute()
	return out.String(), err
}
func TestFreshHomeCanBeConfiguredWithoutDockerOrManualJSON(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	for _, args := range [][]string{{"profile", "create", "basic"}, {"profile", "init", "basic", "--harness", "pi", "--artifact", "harness-config"}, {"profile", "set", "basic"}} {
		if out, err := resourceCLI(t, home, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	resolved, err := artifact.Resolve(home, workspace, "", config.Layer{})
	if err != nil || resolved.Settings.Harness != "pi" || resolved.Profile != "basic" {
		t.Fatal("fresh profile is not usable", err)
	}
	if _, err = os.Stat(filepath.Join(home, "profiles/basic/Dockerfile")); !os.IsNotExist(err) {
		t.Fatal("init implicitly seeded a Dockerfile")
	}
	project := t.TempDir()
	for _, args := range [][]string{{"project", "create", project}, {"project", "init", project, "--harness", "opencode"}} {
		if out, err := resourceCLI(t, home, args...); err != nil {
			t.Fatal(err, out)
		}
	}
	resolved, err = artifact.Resolve(home, project, "", config.Layer{})
	if err != nil || resolved.Settings.Harness != "opencode" {
		t.Fatal("project is not usable", err)
	}
	if out, err := resourceCLI(t, home, "profile", "list", "--json"); err != nil || !strings.Contains(out, `"name":"basic"`) {
		t.Fatal(out, err)
	}
	if out, err := resourceCLI(t, home, "profile", "set", "--clear"); err != nil {
		t.Fatal(out, err)
	}
}
func TestResourceAutomationDoesNotPromptAndScopesNextSteps(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home with spaces")
	out, err := resourceCLI(t, home, "profile", "create", "basic", "--json")
	if err != nil || !strings.Contains(out, `"next_steps"`) || !strings.Contains(out, `"--home"`) {
		t.Fatal(out, err)
	}
	_, err = resourceCLI(t, home, "profile", "init", "basic")
	if err == nil || !strings.Contains(err.Error(), "--harness") {
		t.Fatal("automation guessed a harness", err)
	}
	_, err = resourceCLI(t, home, "profile", "init", "missing", "--harness", "pi")
	if err == nil || !strings.Contains(err.Error(), "profile create missing") || !strings.Contains(err.Error(), shellQuote(home)) {
		t.Fatal("missing owner has no scoped next step", err)
	}
}
func TestPromptParsingAndCancellation(t *testing.T) {
	var out bytes.Buffer
	choices := []string{"pi", "opencode"}
	selected, err := chooseOne(bufio.NewReader(strings.NewReader("2\n")), &out, "Harness", choices)
	if err != nil || selected != "opencode" {
		t.Fatal(selected, err)
	}
	if !strings.Contains(out.String(), "[1]  pi") || !strings.Contains(out.String(), "[2]  opencode") {
		t.Fatal("prompt choices are not bracketed", out.String())
	}
	out.Reset()
	multiple, err := chooseMany(bufio.NewReader(strings.NewReader("1,2,1\n")), &out, "Artifacts", choices)
	if err != nil || len(multiple) != 2 {
		t.Fatal(multiple, err)
	}
	if !strings.Contains(out.String(), "[1]  pi") || !strings.Contains(out.String(), "[2]  opencode") {
		t.Fatal("multi-select choices are not bracketed", out.String())
	}
	_, err = chooseOne(bufio.NewReader(strings.NewReader("garbage\n")), &out, "Harness", choices)
	if err == nil {
		t.Fatal("invalid choice accepted")
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
	quoted := shellQuote(value)
	if quoted != "'/work/a'\"'\"'b $(command)'" {
		t.Fatal(quoted)
	}
}
