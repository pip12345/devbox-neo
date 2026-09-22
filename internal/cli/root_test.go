package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExecutableName(t *testing.T) {
	cmd := New()
	if cmd.Name() != "devbox-neo" {
		t.Fatalf("unexpected executable name: %s", cmd.Name())
	}
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	if !strings.HasPrefix(help, "Persistent development environments\n") {
		t.Fatalf("incorrect help description: %s", help)
	}
	if !strings.Contains(help, "open") || strings.Contains(help, "devbox-neo <target>") || strings.Contains(help, "rewrite") {
		t.Fatalf("incorrect root help: %s", help)
	}
	if strings.Contains(help, "--continue") || strings.Contains(help, "--network string") {
		t.Fatalf("root help contains open-only flags: %s", help)
	}
}

func TestFlagHelpDescribesActions(t *testing.T) {
	for _, tt := range []struct{ command, flag, description string }{
		{"open", "harness-arg", "Pass an argument to the harness (repeatable)"},
		{"recreate", "image", "Rebuild the image without using the build cache"},
		{"recreate", "all", "Recreate all Devbox containers"},
		{"open", "name", "Select the session's folder-local name"},
		{"stop", "force", "Stop even if commands are still running"},
		{"delete", "force", "Allow container deletion despite attached commands; never implies deleting saved data"},
		{"config sources", "show", "Show combined settings and their sources without editing"},
		{"config create", "harness", "Set this config's persistent harness selection"},
		{"config edit", "artifact-harness", "Choose which harness's files to add without changing the config's harness"},
		{"copy", "as", "Destination local name (default: preserve the source name)"},
		{"copy", "move", "Remove the source after the destination is ready"},
		{"delete", "older-than", "Select environments inactive longer than this duration, e.g. 720h; rechecked while locked"},
	} {
		cmd, _, err := New().Find(strings.Fields(tt.command))
		if err != nil {
			t.Fatal(err)
		}
		flag := cmd.Flag(tt.flag)
		if flag == nil || flag.Usage != tt.description {
			t.Fatalf("wrong help for %s --%s: %v", tt.command, tt.flag, flag)
		}
	}
}

func TestCommandHelpDescribesActionsWithoutInitializingHome(t *testing.T) {
	home := t.TempDir()
	before := completionSnapshot(t, home)
	for _, tt := range []struct{ command, description string }{
		{"config create", "Create a config directory and offer initial setup"},
		{"config edit", "Edit a config directory or add missing optional files"},
		{"config sources", "Manage a session's config sources and inspect combined configuration"},
		{"set", "Select or clear a folder's default session"},
		{"shell", "Open a shell in a session"},
		{"exec", "Run a command in a session"},
		{"recreate", "Recreate the container with current settings, keeping session data"},
		{"status", "Show session details, active commands, and pending configuration changes"},

		{"copy", "Copy session state to another folder or local name"},
	} {
		t.Run(tt.command, func(t *testing.T) {
			root := New()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			args := append([]string{"--home", home}, strings.Fields(tt.command)...)
			root.SetArgs(append(args, "--help"))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(out.String(), tt.description) {
				t.Fatalf("wrong help description: %s", out.String())
			}
		})
	}
	if !reflect.DeepEqual(before, completionSnapshot(t, home)) {
		t.Fatal("help initialized home")
	}
}

func TestOpenCommandOwnsTargetAndFlags(t *testing.T) {
	cmd := New()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"open", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	for _, text := range []string{"open <folder|session>", "--continue", "--harness-arg", "--name"} {
		if !strings.Contains(help, text) {
			t.Fatalf("open help is missing %q: %s", text, help)
		}
	}
	for _, flag := range []string{"create", "network", "env", "volume", "port", "docker-arg", "read-only", "harness", "on-exit"} {
		if strings.Contains(help, "--"+flag+" ") {
			t.Fatalf("open help advertises creation flag %q: %s", flag, help)
		}
	}
}

func TestOpenRejectsCreationFlagsBeforeInitialization(t *testing.T) {
	for _, flag := range []string{"--profile=test", "--ignore-project", "--create", "--network=host", "--env=TOKEN=private-value", "--volume=/tmp:/extra", "--port=8080:80", "--docker-arg=--init", "--read-only", "--harness=pi"} {
		t.Run(strings.SplitN(flag, "=", 2)[0], func(t *testing.T) {
			home := t.TempDir()
			before := completionSnapshot(t, home)
			cmd := New()
			out := new(bytes.Buffer)
			cmd.SetOut(out)
			cmd.SetErr(out)
			cmd.SetArgs([]string{"--home", home, "open", ".", flag})
			if code := Execute(context.Background(), cmd); code != 1 || !strings.Contains(out.String(), "Invalid command-line options.") || strings.Contains(out.String(), "private-value") {
				t.Fatalf("unexpected flag handling: exit %d, %s", code, out)
			}
			if !reflect.DeepEqual(before, completionSnapshot(t, home)) {
				t.Fatal("rejected flag initialized home")
			}
		})
	}
}

func TestCreateCommandOwnsCreationButNotLaunchFlags(t *testing.T) {
	cmd, _, err := New().Find([]string{"create"})
	if err != nil || cmd.Name() != "create" {
		t.Fatal(cmd, err)
	}
	for _, flag := range []string{"harness", "network", "env", "volume", "port", "docker-arg", "on-exit", "read-only", "harness-arg"} {
		if cmd.Flags().Lookup(flag) != nil {
			t.Fatal("removed creation flag remains", flag)
		}
	}
	for _, flag := range []string{"create", "continue"} {
		if cmd.Flags().Lookup(flag) != nil {
			t.Fatal("unexpected open-only flag", flag)
		}
	}
	if cmd.Args(cmd, nil) == nil || cmd.Args(cmd, []string{".", "extra"}) == nil || cmd.Args(cmd, []string{"."}) != nil {
		t.Fatal("create must accept exactly one folder")
	}
}

func TestPlainOpenAndStartRejectMissingSessionWithScopedGuidance(t *testing.T) {
	base := t.TempDir()
	current := filepath.Join(base, "current")
	other := filepath.Join(base, "other folder")
	for _, dir := range []string{current, other} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(current); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})
	for _, action := range []string{"open", "start"} {
		for _, selection := range []string{"default", "explicit"} {
			for _, workspace := range []string{".", "../other folder", other} {
				t.Run(action+"/"+selection+"/"+workspace, func(t *testing.T) {
					home := t.TempDir()
					completionFile(t, home, "profiles/test/config.json", `{"version":1,"harness":"pi"}`)
					completionFile(t, home, "config.json", `{"version":1,"default_profile":"test"}`)
					cmd := New()
					out := new(bytes.Buffer)
					cmd.SetOut(out)
					cmd.SetErr(out)
					args := []string{"--home", home, action, workspace}
					if selection == "explicit" {
						args = append(args, "--name", "test")
					}
					cmd.SetArgs(args)
					if code := Execute(context.Background(), cmd); code != 1 {
						t.Fatal(code)
					}
					message := "No sessions for this folder."
					if selection == "explicit" {
						message = "No session named \"test\" exists for this folder."
					}
					want := "Error: " + message + "\nTarget: " + workspace + "\n\n" +
						"Create a session:\n  devbox-neo --home " + home + " create " + shellQuote(workspace) + "\n"
					if selection != "explicit" {
						want += "\nThen select a default:\n  devbox-neo --home " + home + " set " + shellQuote(workspace) + "\n" +
							"\nThen open it:\n  devbox-neo --home " + home + " open " + shellQuote(workspace) + "\n"
					}
					if out.String() != want {
						t.Fatalf("got %q; want %q", out.String(), want)
					}
				})
			}
		}
	}
}

func TestRootRejectsImplicitTarget(t *testing.T) {
	cmd := New()
	cmd.SetArgs([]string{"workspace"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("implicit target was not rejected: %v", err)
	}
}

func TestSmoke(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}} {
		cmd := New()
		out := new(bytes.Buffer)
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(out.String()) == "" {
			t.Fatal("empty output")
		}
	}
}
