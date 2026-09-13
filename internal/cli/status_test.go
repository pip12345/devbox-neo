package cli

import (
	"bytes"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/environment"
	"devbox/internal/store"
	"github.com/spf13/cobra"
)

func TestStatusTableSeparatesLiveStateFromChanges(t *testing.T) {
	views := []app.View{
		{Name: "clean", Exists: true, Running: true, Desired: environment.NoChange},
		{Name: "runtime", Exists: true, Desired: environment.RuntimeSync},
		{Name: "container", Exists: true, Running: true, Desired: environment.Recreate},
		{Name: "image", Exists: true, Desired: environment.RebuildAndRecreate},
		{Name: "invalid", Exists: true, Running: true, ConfigError: "invalid config\nnext line"},
		{Name: "broken", Exists: true, Error: "corrupt record"},
		{Name: "pending", Exists: true, Pending: &store.Reservation{Mode: "clone", Phase: "prepare"}},
		{Name: "unknown", Exists: true},
	}
	var out bytes.Buffer
	if err := printStatusList(&out, views); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"NAME", "CONTAINER", "CHANGE", "No changes", "Runtime changes", "Recreate needed", "Rebuild + recreate needed", "corrupt record", `invalid config\nnext line`, "devbox-neo recreate container", "devbox-neo recreate image"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Count(text, "Cannot check") != 4 || strings.Count(text, "devbox-neo recreate") != 2 || strings.Contains(text, "--image") || strings.Contains(text, "\x1b") {
		t.Fatal("incorrect unknown states, actions or escaping", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "invalid ") && (!strings.Contains(line, "running") || strings.Contains(line, "running!")) {
			t.Fatal("desired error changed live state", line)
		}
	}
}

func TestStatusRejectsAmbiguousSelectionBeforeInitialization(t *testing.T) {
	for _, args := range [][]string{{"status"}, {"status", "target", "--all"}, {"status", "one", "two"}} {
		root := &cobra.Command{Use: "devbox-neo", SilenceErrors: true, SilenceUsage: true}
		profile := ""
		root.AddCommand(sessionCommands(func(*cobra.Command) (*app.Engine, error) {
			t.Fatal("invalid status selection initialized the home")
			return nil, nil
		}, &profile)...)
		root.SetArgs(args)
		if err := root.Execute(); err == nil {
			t.Fatal("invalid selection accepted", args)
		}
	}
}

func TestStatusAllCompletionDoesNotSuggestTargets(t *testing.T) {
	root := New()
	status, _, err := root.Find([]string{"status"})
	if err != nil {
		t.Fatal(err)
	}
	if err := status.Flags().Set("all", "true"); err != nil {
		t.Fatal(err)
	}
	values, directive := status.ValidArgsFunction(status, nil, "")
	if len(values) != 0 || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatal("--all offered exact targets", values, directive)
	}
}
