package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"devbox/internal/app"
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

func TestConfigDeleteRequiresConfirmationAndReportsJSON(t *testing.T) {
	home := t.TempDir()
	if out, err := resourceCLI(t, home, "config", "create", "base", "--json"); err != nil {
		t.Fatal(out, err)
	}
	path := filepath.Join(home, "configs", "base")
	for _, args := range [][]string{{"config", "delete", "base"}, {"config", "delete", "base", "--json"}} {
		if out, err := resourceCLI(t, home, args...); err == nil || !strings.Contains(out, "requires --force") {
			t.Fatal("script deletion had no confirmation gate", out, err)
		}
	}
	master, slave := testTerminal(t)
	master.WriteString("n\n")
	cmd := New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(slave)
	cmd.SetArgs([]string{"--home", home, "config", "delete", "base"})
	if err := cmd.ExecuteContext(context.Background()); err != nil || !strings.Contains(out.String(), "Delete config base and all files") || !strings.Contains(out.String(), "Cancelled.") {
		t.Fatal(out.String(), err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("cancellation deleted config", err)
	}
	outText, err := resourceCLI(t, home, "config", "delete", "base", "--force", "--json")
	var result resource.Result
	if err != nil || json.Unmarshal([]byte(outText), &result) != nil || !slices.Equal(result.Deleted, []string{path}) {
		t.Fatal(outText, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("confirmed deletion retained config", err)
	}
}

func TestConfigDeleteRefusesDesiredAndCommittedSessionSources(t *testing.T) {
	e, _, name := namedCLIFixture(t)
	ctx := context.Background()
	service := resource.Service{Home: e.Store.Home}
	if out, err := resourceCLI(t, e.Store.Home, "config", "delete", "base", "--force"); err == nil || !strings.Contains(out, "Used by "+name+":") {
		t.Fatal("--force bypassed a saved session reference", out, err)
	}
	for _, phase := range []string{"selected", "recorded"} {
		_, err := service.DeleteConfig(ctx, "base")
		var blocked *commanderror.Error
		if !errors.As(err, &blocked) || blocked.Code != "config_in_use" || len(blocked.Next) != 1 || blocked.Next[0].Reason != "Used by "+name {
			t.Fatal("deleted a config used by a session", phase, err)
		}
		if phase == "selected" {
			shown, err := e.Store.Read(ctx, name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.UpdateSources(ctx, shown, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := e.Delete(ctx, app.DeleteOptions{Selection: app.Selection{Targets: []string{name}}, Scope: app.DeleteSession}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteConfig(ctx, "base"); err != nil {
		t.Fatal("unreferenced config cannot be deleted", err)
	}
}

func TestConfigUsageReportsEveryDesiredAndCommittedSession(t *testing.T) {
	e, q, first := namedCLIFixture(t)
	ctx := context.Background()
	service := resource.Service{Home: e.Store.Home}
	base := slices.Clone(q.Sources)
	q.LocalName = "Second"
	second, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	secondRecord, err := e.Store.Read(ctx, second.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.UpdateSources(ctx, secondRecord, nil); err != nil {
		t.Fatal(err)
	}
	alt, err := service.ConfigDirectory("alternate", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateConfig(ctx, alt, resource.SetupOptions{Harness: harnessSetting("pi")}); err != nil {
		t.Fatal(err)
	}
	q.LocalName, q.Sources = "Third", testConfigSources(e.Store.Home, "alternate")
	third, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	thirdRecord, err := e.Store.Read(ctx, third.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.UpdateSources(ctx, thirdRecord, base); err != nil {
		t.Fatal(err)
	}
	owner, err := service.ConfigDirectory("base", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	users, err := service.ConfigUsers(ctx, owner)
	if err != nil || len(users) != 3 {
		t.Fatal(users, err)
	}
	uses := map[string]resource.ConfigUse{}
	for i, user := range users {
		if i > 0 && users[i-1].Session >= user.Session {
			t.Fatal("usage order is not stable", users)
		}
		uses[user.Session] = user
	}
	if !uses[first].Desired || !uses[first].Committed || uses[second.Name].Desired || !uses[second.Name].Committed || !uses[third.Name].Desired || uses[third.Name].Committed {
		t.Fatal("incomplete usage report", uses)
	}
	output, err := resourceCLI(t, e.Store.Home, "config", "delete", "base", "--force")
	if err == nil {
		t.Fatal("deleted used config", output)
	}
	for name := range uses {
		if !strings.Contains(output, "Used by "+name+":") {
			t.Fatal("deletion did not identify every session", name, output)
		}
	}
	if strings.Contains(output, " (desired") || strings.Contains(output, " (committed") {
		t.Fatal("deletion exposed internal source states", output)
	}
	master, slave := testTerminal(t)
	if _, err := master.WriteString("0\n"); err != nil {
		t.Fatal(err)
	}
	cmd := New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(slave)
	cmd.SetArgs([]string{"--home", e.Store.Home, "config", "edit", "base"})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	for name := range uses {
		if !strings.Contains(out.String(), "  "+name+"\n") {
			t.Fatal("editor did not identify every session", name, out.String())
		}
	}
	if strings.Contains(out.String(), " (desired") || strings.Contains(out.String(), " (committed") {
		t.Fatal("editor exposed internal source states", out.String())
	}
	if !strings.Contains(out.String(), "Used by saved sessions:") {
		t.Fatal(out.String())
	}
	corrupt := filepath.Join(e.Store.Home, "sessions", "devbox-unreadable")
	if err := os.Mkdir(corrupt, 0700); err != nil {
		t.Fatal(err)
	}
	output, err = resourceCLI(t, e.Store.Home, "config", "delete", "base", "--force")
	var blocked *commanderror.Error
	if !errors.As(err, &blocked) || blocked.Code != "config_usage_unknown" || len(blocked.Next) != 4 {
		t.Fatal("incomplete inventory was treated as a complete usage report", output, err)
	}
	for name := range uses {
		if !strings.Contains(output, name) {
			t.Fatal("known users disappeared from incomplete report", name, output)
		}
	}
	master, slave = testTerminal(t)
	if _, err := master.WriteString("0\n"); err != nil {
		t.Fatal(err)
	}
	cmd = New()
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(slave)
	cmd.SetArgs([]string{"--home", e.Store.Home, "config", "edit", "base"})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	if !strings.Contains(out.String(), "Shared-use report is incomplete:") {
		t.Fatal("editor hid incomplete inventory", out.String())
	}
	for name := range uses {
		if !strings.Contains(out.String(), name) {
			t.Fatal("editor hid known users during incomplete inventory", name, out.String())
		}
	}
}

func TestConfigListUsesSelectedHomeAndReportsInvalidEntries(t *testing.T) {
	home := filepath.Join(t.TempDir(), "custom home")
	t.Setenv("PATH", t.TempDir()) // Listing does not need Docker.
	out, err := resourceCLI(t, home, "config", "list")
	if err != nil || strings.HasPrefix(out, "\n") || strings.Contains(out, "\n\n") || !strings.Contains(strings.ReplaceAll(out, "\n", ""), filepath.Join(home, "configs")) || !strings.Contains(out, "No named configs.") || !strings.Contains(out, "config create base") || !strings.Contains(out, shellQuote(home)) {
		t.Fatal(out, err)
	}
	if out, err = resourceCLI(t, home, "config", "list", "--json"); err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatal(out, err)
	}
	if out, err = resourceCLI(t, home, "config", "create", "base", "--harness", "pi"); err != nil {
		t.Fatal(out, err)
	}
	broken := filepath.Join(home, "configs", "broken")
	if err := os.Mkdir(broken, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "config.json"), []byte(`{"version":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = resourceCLI(t, home, "config", "list")
	if err != nil || strings.HasPrefix(out, "\n") || strings.Contains(out, "\n\n") || !strings.HasSuffix(out, "\n") || !strings.Contains(strings.ReplaceAll(out, "\n", ""), filepath.Join(home, "configs")) || !strings.Contains(out, "NAME") || !strings.Contains(out, "HARNESS") || !strings.Contains(out, "PATH") || !strings.Contains(out, filepath.Join(home, "configs", "base")) || !strings.Contains(out, filepath.Join(home, "configs", "broken")) || !strings.Contains(out, "pi") || !strings.Contains(out, "! broken:") || strings.Contains(out, "readable") {
		t.Fatal(out, err)
	}
	out, err = resourceCLI(t, home, "config", "list", "--json")
	var entries []resource.ConfigEntry
	if err != nil || json.Unmarshal([]byte(out), &entries) != nil || len(entries) != 2 || entries[0].Name != "base" || entries[1].Name != "broken" || entries[0].Harness != "pi" || entries[1].Error == "" {
		t.Fatal(out, err)
	}
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
