package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

func TestConfigCreationOverviewKeepsEditableDraftUntilCreate(t *testing.T) {
	s := menuService(t)
	cwd, userHome := t.TempDir(), t.TempDir()
	taken, err := s.ConfigDirectory("taken", cwd, userHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateConfig(context.Background(), taken, resource.SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	// Choose a harness and files before renaming. Back out of changes to the
	// files, harness and destination; none should replace the accepted choices.
	input := &workflowInput{lines: []string{"2\n", "1\n", "3\n", "2\n", "5\n", "1\n", "fresh\n", "3\n", "2\n", "0\n", "2\n", "0\n", "1\n", "taken\n", ":back\n", "4\n"}}
	input.before = func(int) {
		for _, name := range []string{"first", "fresh"} {
			if _, err := os.Stat(filepath.Join(s.Home, "configs", name)); !os.IsNotExist(err) {
				t.Fatal("draft created files before Create config", name, err)
			}
		}
	}
	var out bytes.Buffer
	owner, _, created, err := createConfig(testMenu(context.Background(), input, &out), s, "first", cwd, userHome)
	if err != nil || !created || owner.Name != "fresh" {
		t.Fatal(owner, created, err, out.String())
	}
	source, err := s.ConfigSource(owner)
	if err != nil || string(source["harness"]) != `"claude"` {
		t.Fatal(source, err)
	}
	if _, err := os.Stat(filepath.Join(owner.Root, "setup.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Home, "configs", "first")); !os.IsNotExist(err) {
		t.Fatal("original destination was created", err)
	}
	for _, want := range []string{"Name/location: first", "Name/location: fresh", "Harness: claude", "Optional files: setup.sh", "Config already exists", "[4]  Create config"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("missing draft feedback", want, out.String())
		}
	}
}

func TestBareConfigCreateOpensOverviewWithoutForcedSetup(t *testing.T) {
	s := menuService(t)
	master, slave := testTerminal(t)
	if _, err := master.WriteString("4\n1\nbasic\n4\n"); err != nil {
		t.Fatal(err)
	}
	cmd := directoryCommand(func(*cobra.Command) (*resource.Service, error) { return s, nil }, true)
	var out bytes.Buffer
	cmd.SetIn(slave)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err, out.String())
	}
	for _, want := range []string{"Name/location: Unset", "Set a config name or directory first.", "Name/location: basic", "Harness: Unset", "Optional files: None"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("missing overview detail", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Select a harness") || strings.Contains(out.String(), "Choose optional files") {
		t.Fatal("creation forced optional screens", out.String())
	}
	owner := testConfigOwner(t, s.Home, "basic")
	source, err := s.ConfigSource(owner)
	if err != nil || source["harness"] != nil {
		t.Fatal("creation chose a harness implicitly", source, err)
	}
	files, err := os.ReadDir(owner.Root)
	if err != nil || len(files) != 1 || files[0].Name() != "config.json" {
		t.Fatal("creation added optional files implicitly", files, err)
	}
}

func TestConfigCreateWithoutDestinationRequiresInteractiveSetup(t *testing.T) {
	for _, args := range [][]string{{}, {"--json"}, {"--harness", "pi"}, {"--artifact", "setup.sh"}, {"--artifact-harness", "pi"}} {
		cmd := directoryCommand(func(*cobra.Command) (*resource.Service, error) {
			t.Fatal("invalid invocation initialized resource service")
			return nil, nil
		}, true)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(""))
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "requires a name or path") {
			t.Fatal(args, err, out.String())
		}
	}
}
