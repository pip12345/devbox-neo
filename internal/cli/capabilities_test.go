package cli

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// This is a coverage contract, not a registry generating either interface.
// Command syntax lives in docs/src/reference/commands.md. Behavior tests
// exercise shared results separately; file editing deliberately needs no set CLI.
func TestInteractiveCapabilitiesHaveDirectCommandInputs(t *testing.T) {
	for _, capability := range []struct {
		command string
		flags   []string
	}{
		{"create", []string{"name", "config"}},
		{"open", []string{"name", "continue", "harness-arg"}},
		{"start", []string{"name"}}, {"stop", []string{"name", "force"}},
		{"shell", []string{"name"}}, {"exec", []string{"name"}},
		{"logs", []string{"name", "tail", "follow"}},
		{"list", []string{"sort", "wide", "json"}}, {"status", []string{"name", "json"}},
		{"recreate", []string{"name", "all", "image"}},
		{"ssh", []string{"name", "host-master"}},
		{"copy", []string{"name", "as", "move", "dry-run", "json"}},
		{"delete", []string{"name", "container", "session", "all", "stopped", "orphaned", "older-than", "force", "dry-run", "json"}},
		{"network inspect", []string{"name"}}, {"network env", []string{"name", "get"}},
		{"network connect", []string{"name"}}, {"network disconnect", []string{"name"}},
		{"edit", []string{"name", "config", "show", "default", "clear-default", "json"}},
		{"config create", []string{"harness", "artifact", "artifact-harness", "json"}},
		{"config edit", []string{"harness", "artifact", "artifact-harness", "json"}},
		{"config list", []string{"json"}}, {"config show", []string{"json"}}, {"config users", []string{"json"}},
		{"config delete", []string{"force", "json"}},
		{"completion bash", nil}, {"completion zsh", nil}, {"completion fish", nil}, {"completion powershell", nil},
		{"version", nil},
	} {
		t.Run(capability.command, func(t *testing.T) {
			cmd, rest, err := New().Find(strings.Fields(capability.command))
			if err != nil || len(rest) != 0 || cmd.RunE == nil && cmd.Run == nil {
				t.Fatal("missing direct command", rest, err)
			}
			for _, flag := range capability.flags {
				if cmd.Flag(flag) == nil {
					t.Fatal("missing direct input", flag)
				}
			}
		})
	}
}

func TestNewConfigCompletionIsReadOnly(t *testing.T) {
	home := filepath.Join(t.TempDir(), "uninitialized")
	completionFile(t, home, "configs/base/config.json", `{"version":1}`)
	before := completionSnapshot(t, home)
	for _, path := range []string{"config show", "config users"} {
		args := append([]string{"--home", home}, strings.Fields(path)...)
		values, _ := runCompletion(t, append(args, "ba")...)
		if !slices.Contains(values, "base") {
			t.Fatal(path, values)
		}
	}
	if !reflect.DeepEqual(before, completionSnapshot(t, home)) {
		t.Fatal("completion initialized or locked state")
	}
}
