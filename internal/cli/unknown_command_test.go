package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnknownCommandsUseStructuredSuggestionsWithoutInitializingHome(t *testing.T) {
	for _, args := range [][]string{{"/tmp"}, {"awerhkjawer"}, {"config", "creat"}, {"network", "inspec"}, {"bad\nError: forged\x1b[31m"}} {
		home := filepath.Join(t.TempDir(), "absent")
		root := New()
		var out, stderr bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&stderr)
		root.SetArgs(append([]string{"--home", home}, args...))
		if code := Execute(context.Background(), root); code == 0 {
			t.Fatal("unknown command succeeded", args)
		}
		text := stderr.String()
		if !strings.HasPrefix(text, "Error: unknown command ") {
			t.Fatal(text)
		}
		if strings.Contains(text, `\n\nDid you mean`) || strings.Contains(text, "\x1b") || strings.Contains(text, "\nError: forged") {
			t.Fatal("bad escaping", text)
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatal("initialized home", err)
		}
		if args[0] == "/tmp" && !strings.Contains(text, "Did you mean:\n  dbx --home "+home+" stop\n") {
			t.Fatal("missing structured suggestion", text)
		}
		if args[0] == "config" && !strings.Contains(text, " config create\n") {
			t.Fatal(text)
		}
		if args[0] == "network" && !strings.Contains(text, " network inspect\n") {
			t.Fatal(text)
		}
	}
}

func TestGroupCommandsStillShowHelpWithoutArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"config"}, {"network"}} {
		root := New()
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs(args)
		if code := Execute(context.Background(), root); code != 0 {
			t.Fatal(code, output.String())
		}
		if !strings.Contains(output.String(), "Available Commands:") {
			t.Fatal(output.String())
		}
	}
}
