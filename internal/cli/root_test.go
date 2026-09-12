package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

func TestOpenCommandOwnsTargetAndFlags(t *testing.T) {
	cmd := New()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"open", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	for _, text := range []string{"open <target>", "--create", "--continue", "--network string"} {
		if !strings.Contains(help, text) {
			t.Fatalf("open help is missing %q: %s", text, help)
		}
	}
}

func TestCreateCommandOwnsCreationButNotLaunchFlags(t *testing.T) {
	cmd, _, err := New().Find([]string{"create"})
	if err != nil || cmd.Name() != "create" {
		t.Fatal(cmd, err)
	}
	for _, flag := range []string{"harness", "network", "env", "volume", "port", "docker-arg", "on-exit", "read-only", "harness-arg"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatal("missing creation flag", flag)
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
	for _, action := range []string{"open", "start"} {
		t.Run(action, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			profileDir := filepath.Join(home, "profiles/test")
			if err := os.MkdirAll(profileDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(profileDir, "config.json"), []byte(`{"version":1,"harness":"pi"}`), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := New()
			out := new(bytes.Buffer)
			cmd.SetOut(out)
			cmd.SetErr(out)
			cmd.SetArgs([]string{"--home", home, action, workspace, "--profile", "test"})
			if code := Execute(context.Background(), cmd); code != 1 {
				t.Fatal(code)
			}
			for _, want := range []string{"Error: No environment exists (profile: test).", "devbox-neo --home " + home + " create " + workspace + " --profile test", "open " + workspace + " --create --profile test"} {
				if !strings.Contains(out.String(), want) {
					t.Fatal("missing guidance", want, out.String())
				}
			}
		})
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
