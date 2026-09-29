package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestBuiltinAndUserOverrideUseSameSchema(t *testing.T) {
	home := t.TempDir()
	builtin, err := Load(home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if builtin.Origin != "builtin" || len(builtin.Definition.Merge) != 2 || builtin.Definition.Merge[0].Strategy != "json-keys" {
		t.Fatal("Pi did not use parsed declarations")
	}
	custom := builtin.Definition
	custom.Binary = "custom-pi"
	custom.Stores[0].Target = "/home/${user}/.custom"
	p := filepath.Join(home, "harnesses/pi/harness.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	b, _ := json.Marshal(custom)
	os.WriteFile(p, b, 0600)
	effective, err := Load(home, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if effective.Origin != p || effective.Definition.Binary != "custom-pi" || effective.Definition.Stores[0].Target != "/home/devuser/.custom" || effective.Hash == builtin.Hash {
		t.Fatal("user override/template was not applied")
	}
	if len(effective.Defaults) != 0 {
		t.Fatal("user override inherited builtin defaults")
	}
	os.WriteFile(p, []byte(`invalid`), 0600)
	if _, err = Load(home, "pi"); err == nil {
		t.Fatal("invalid override fell back to builtin")
	}
}
func TestUnsafeAndOverlappingDeclarations(t *testing.T) {
	for _, mutate := range []func(*Definition){
		func(d *Definition) { d.Stores[0].Target = "/" },
		func(d *Definition) { d.Stores[1].Target = d.Stores[0].Target + "/nested" },
		func(d *Definition) { d.Config.Path = "../escape" },
		func(d *Definition) { d.Auth[0].Source = "../escape" },
		func(d *Definition) { d.Merge[0].Keys = []string{"same", "same"} },
		func(d *Definition) { d.Merge = append(d.Merge, d.Merge[0]) },
	} {
		h, err := Load(t.TempDir(), "pi")
		if err != nil {
			t.Fatal(err)
		}
		mutate(&h.Definition)
		if err = h.Definition.Validate(); err == nil {
			t.Fatal("unsafe declaration accepted")
		}
	}
}
func TestRegistryReportsBrokenOverridesWithoutHidingValidChoices(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "harnesses/pi/harness.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte("bad"), 0600)
	h, err := Load(home, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	if h.Definition.Config.Store != "config" || len(h.Definition.Stores) != 3 || len(h.Definition.Merge) != 0 {
		t.Fatal("unexpected OpenCode declarations")
	}
	custom := h.Definition
	custom.Name = "third"
	custom.Install.Script, custom.Install.Shell = "", "true"
	b, _ := json.Marshal(custom)
	p = filepath.Join(home, "harnesses/third/harness.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, b, 0600)
	registry, err := Enumerate(home)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range registry.Valid {
		names = append(names, entry.Definition.Name)
	}
	if !slices.Equal(names, []string{"claude", "opencode", "third"}) || len(registry.Invalid) != 1 || registry.Invalid[0].Name != "pi" {
		t.Fatal("registry hid an invalid override or valid choice", registry)
	}
}
func TestBuiltinClaude(t *testing.T) {
	h, err := Load(t.TempDir(), "claude")
	if err != nil {
		t.Fatal(err)
	}
	d := h.Definition
	if h.Origin != "builtin" || d.Binary != "claude" || !strings.Contains(d.Install.Shell, "https://claude.ai/install.sh") || !slices.Equal(d.Install.Path, []string{"/home/devuser/.local/bin"}) {
		t.Fatal("Claude must load and install through the built-in definition", h)
	}
	if !slices.Equal(d.Launch.Args, []string{"--dangerously-skip-permissions"}) || !slices.Equal(d.Launch.Continue, []string{"--continue"}) || !d.Session.Clone || !d.Session.Relocate {
		t.Fatal("Claude launch or transfer declarations changed", d)
	}
	if !reflect.DeepEqual(d.Stores, []Store{{Name: "home", Scope: "environment", Target: "/home/devuser/.claude"}}) || d.Config != (Config{Store: "home", Path: "."}) {
		t.Fatal("Claude state must use its declared environment store", d)
	}
	wantAuth := []Auth{
		{Source: ".credentials.json", Target: "/home/devuser/.claude/.credentials.json", Kind: "file", Create: true},
		{Source: ".claude.json", Target: "/home/devuser/.claude.json", Kind: "file", Create: true},
	}
	if !reflect.DeepEqual(d.Auth, wantAuth) || !reflect.DeepEqual(d.Merge, []Merge{{Path: "settings.json", Strategy: "json-keys", Keys: []string{"tui", "pluginConfigs"}}}) {
		t.Fatal("Claude auth or managed-key declarations changed", d)
	}
	if len(h.Defaults) != 2 || strings.TrimSpace(string(h.Defaults["CLAUDE.md"].Data)) != "@/devbox/AGENTS.md" {
		t.Fatal("Claude must inherit embedded Devbox guidance", h.Defaults)
	}
	var settings struct {
		TUI     string `json:"tui"`
		Plugins map[string]struct {
			Options struct {
				InstructionFiles string `json:"instructionFiles"`
			} `json:"options"`
		} `json:"pluginConfigs"`
	}
	if err := json.Unmarshal(h.Defaults["settings.json"].Data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.TUI != "fullscreen" || settings.Plugins["agents-md@builtin"].Options.InstructionFiles != "claude-md-and-agents-md" {
		t.Fatal("Claude defaults lost fullscreen or instruction-file settings", settings)
	}
}

func TestBuiltinOpenCodeV2(t *testing.T) {
	h, err := Load(t.TempDir(), "opencode")
	if err != nil {
		t.Fatal(err)
	}
	d := h.Definition
	if d.Install.Script != "install.sh" || d.Install.Shell != "" || len(h.InstallFiles) != 2 {
		t.Fatal("OpenCode must own its installer and wrapper as image files", d.Install)
	}
	install := string(h.InstallFiles[d.Install.Script].Data)
	for _, want := range []string{"--branch v2", "4c33a253aa89ec0fa4faaa5d5b4aefef7d1a3963", "--frozen-lockfile", "--skip-install", "opencode-auth.sh", "opencode-native", "nodejs", "setup_22.x", "bun-v1.4.2"} {
		if !strings.Contains(install, want) {
			t.Fatal("OpenCode must build the pinned merged credential API and install its wrapper", want)
		}
	}
	for name, file := range h.InstallFiles {
		command := exec.Command("bash", "-n")
		command.Stdin = strings.NewReader(string(file.Data))
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("invalid OpenCode installation file %s: %v\n%s", name, err, output)
		}
	}
	if d.Binary != "opencode" || len(d.Launch.Continue) != 1 || d.Launch.Continue[0] != "-c" {
		t.Fatal("OpenCode must retain its launch and continuation commands", d)
	}
	if len(d.Auth) != 1 || d.Auth[0].Source != "shared" || d.Auth[0].Kind != "directory" || d.Auth[0].Target != "/home/devuser/.local/share/devbox-opencode-auth" || !d.Auth[0].Create {
		t.Fatal("OpenCode must mount a shared auth directory for atomic snapshot publication", d.Auth)
	}
	if !d.Session.Clone || !d.Session.Relocate {
		t.Fatal("OpenCode must preserve session transfer capabilities")
	}
	var config struct {
		Update       string `json:"update"`
		Share        string `json:"share"`
		Experimental struct {
			Policies []struct {
				Action   string `json:"action"`
				Resource string `json:"resource"`
				Effect   string `json:"effect"`
			} `json:"policies"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal([]byte(d.Env["OPENCODE_CONFIG_CONTENT"]), &config); err != nil {
		t.Fatal(err)
	}
	if config.Update != "disable" || config.Share != "disabled" || len(config.Experimental.Policies) != 1 || config.Experimental.Policies[0].Action != "provider.use" || config.Experimental.Policies[0].Resource != "opencode" || config.Experimental.Policies[0].Effect != "deny" {
		t.Fatal("OpenCode v2 defaults must keep sharing and the OpenCode provider disabled", config)
	}
}

func TestEmbeddedAndHostDefaultsShareRecursiveFileRules(t *testing.T) {
	tree, err := readTree(fstest.MapFS{
		"nested/file": {Data: []byte("value"), Mode: 0700},
		"link":        {Mode: os.ModeSymlink},
		"pipe":        {Mode: os.ModeNamedPipe},
	}, "defaults")
	if err != nil || len(tree.Files) != 1 || string(tree.Files["nested/file"].Data) != "value" || tree.Files["nested/file"].Mode != 0700 {
		t.Fatal(tree, err)
	}
	if len(tree.Warnings) != 2 {
		t.Fatal("unsupported entries must each produce a warning", tree.Warnings)
	}
}
func TestCanonicalPathsAndAuthCannotObscureStores(t *testing.T) {
	for _, change := range []func(*Definition){func(d *Definition) { d.Config.Path = "a/../b" }, func(d *Definition) { d.Auth[0].Target = "/home/devuser/.pi"; d.Auth[0].Kind = "directory" }} {
		h, err := Load(t.TempDir(), "pi")
		if err != nil {
			t.Fatal(err)
		}
		change(&h.Definition)
		if err = h.Definition.Validate(); err == nil {
			t.Fatal("ambiguous path or obscured store accepted")
		}
	}
}
func TestInvalidUnselectedDefinitionDoesNotBlockSelection(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "harnesses/broken/harness.json")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte("bad"), 0600)
	if _, err := Load(home, "pi"); err != nil {
		t.Fatal(err)
	}
}
