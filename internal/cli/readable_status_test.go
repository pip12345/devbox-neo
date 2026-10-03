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

func TestStatusUsesWorkspaceAndNameInsteadOfTargetAsName(t *testing.T) {
	var out bytes.Buffer
	views := []app.View{
		{Target: strings.Repeat("a", 32), SessionID: strings.Repeat("a", 32), Workspace: "/work/api", LocalName: "main", Harness: "pi", Exists: true, Desired: environment.NoChange},
		{Target: strings.Repeat("b", 32), SessionID: strings.Repeat("b", 32), Workspace: "/work/web", LocalName: "main", Harness: "pi", ImageMissing: true, Desired: environment.NoChange},
	}
	if err := printStatusList(&cobra.Command{}, &out, views, ""); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"FOLDER", "NAME", "HARNESS", "/work/api", "/work/web", "main", "recorded image missing", "container missing"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing readable status context", want, text)
		}
	}
	if strings.Contains(text, views[0].Target) || strings.Count(text, views[1].Target) != 1 || !strings.Contains(text, "dbx recreate "+views[1].Target) {
		t.Fatal("clean rows display IDs as names", text)
	}
}

func TestStatusMissingRuntimeGuidance(t *testing.T) {
	for _, tc := range []struct {
		name         string
		view         app.View
		wantRecreate bool
	}{
		{"container", app.View{Desired: environment.NoChange}, true},
		{"both", app.View{ImageMissing: true, Desired: environment.NoChange}, true},
		{"managed config", app.View{Desired: environment.RuntimeSync, PendingInputChanges: []environment.InputChange{{Scope: environment.RuntimeScope, Field: "managed_config", Code: "input_changed"}}}, true},
		{"invalid config", app.View{ConfigError: "repair current config"}, true},
		{"image only", app.View{Exists: true, ImageMissing: true, Desired: environment.NoChange}, false},
		{"pending", app.View{Pending: &store.Reservation{Mode: "clone", Phase: "committed"}}, false},
		{"corrupt", app.View{Error: "corrupt record"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := tc.view
			view.Target, view.SessionID = strings.Repeat("a", 32), strings.Repeat("a", 32)
			view.Workspace, view.LocalName = "/work/api", "main"
			cmd := &cobra.Command{}
			var home string
			cmd.Flags().StringVar(&home, "home", "", "")
			if err := cmd.Flags().Set("home", "/custom home"); err != nil {
				t.Fatal(err)
			}
			for _, bulk := range []bool{false, true} {
				var out bytes.Buffer
				var err error
				if bulk {
					err = printStatusList(cmd, &out, []app.View{view}, home)
				} else {
					err = printStatusDetails(&out, app.StatusDetails{View: view}, statusRecreateSteps(cmd, view, home))
				}
				if err != nil {
					t.Fatal(err)
				}
				text := out.String()
				command := "dbx --home '/custom home' recreate " + view.Target
				want := 0
				if tc.wantRecreate {
					want = 1
				}
				if strings.Count(text, command) != want {
					t.Fatal("wrong scoped guidance", bulk, text)
				}
				for _, stale := range []string{"access will rebuild", "runtime will rebuild", "Changes apply on container restart."} {
					if strings.Contains(text, stale) {
						t.Fatal("misleading missing-runtime guidance", text)
					}
				}
				if tc.wantRecreate && !strings.Contains(text, "explicit Recreate") {
					t.Fatal(text)
				}
				if view.ImageMissing && !strings.Contains(text, "existing container access is unaffected") {
					t.Fatal(text)
				}
				if view.ConfigError != "" && !strings.Contains(text, view.ConfigError) {
					t.Fatal(text)
				}
			}
		})
	}
}

func TestCompletionDescriptionsDoNotBecomeTargets(t *testing.T) {
	values := []string{"aaaa\t/work/api / main", "bbbb\t/work/web / main", "bad\nrow\tdescription"}
	if got := completionMatches(values, []string{"aaaa"}, ""); len(got) != 1 || got[0] != values[1] {
		t.Fatal("used ID did not filter described candidate", got)
	}
}
