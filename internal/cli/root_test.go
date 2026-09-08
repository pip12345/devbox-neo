package cli

import (
	"bytes"
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
	for _, text := range []string{"open <target>", "--continue", "--network string"} {
		if !strings.Contains(help, text) {
			t.Fatalf("open help is missing %q: %s", text, help)
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
