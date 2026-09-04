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
	if !strings.Contains(out.String(), "devbox-neo <target>") || strings.Contains(out.String(), "devbox-rewrite") {
		t.Fatalf("incorrect help command name: %s", out.String())
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
