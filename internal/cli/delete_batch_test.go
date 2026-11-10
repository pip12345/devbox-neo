package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"github.com/spf13/cobra"
)

func TestDeleteBatchCLIRetainsResultsAndErrors(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		for _, dryRun := range []bool{false, true} {
			t.Run(map[bool]string{false: "human", true: "json"}[asJSON]+map[bool]string{false: "/execute", true: "/preview"}[dryRun], func(t *testing.T) {
				e, q, first := namedCLIFixture(t)
				q.LocalName = "Second"
				second, err := e.Create(context.Background(), q)
				if err != nil {
					t.Fatal(err)
				}
				firstID := sessionRecord(t, e, first).Applied.ContainerID
				failure := &docker.ExitError{Code: 41, Operation: "container"}
				cause := commanderror.New("inspection_test_failure", "Cannot inspect selected runtime.", first, failure,
					commanderror.Next("Inspect session", "status", first))
				d := e.Docker.Runner.(*dockertest.Daemon)
				d.Fail = func(args []string) error {
					if slices.Equal(args, []string{"container", "inspect", firstID}) {
						return cause
					}
					return nil
				}
				local := ""
				cmd := deleteCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &local)
				cmd.Flags().String("home", "", "")
				cmd.SilenceErrors, cmd.SilenceUsage = true, true
				var out, stderr bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&stderr)
				args := []string{"--all", "--session", "--home", e.Store.Home}
				if asJSON {
					args = append(args, "--json")
				}
				if dryRun {
					args = append(args, "--dry-run")
				}
				cmd.SetArgs(args)
				if code := Execute(context.Background(), cmd); code != 41 {
					t.Fatal(code, out.String(), stderr.String())
				}
				if asJSON {
					var report errorReport
					decoder := json.NewDecoder(&out)
					if err := decoder.Decode(&report); err != nil {
						t.Fatal(out.String(), err)
					}
					if report.Batch == nil || len(report.Batch.Results) != 2 || report.Batch.Summary.Failed != 1 || report.Batch.Summary.Succeeded != 1 || (report.Partial != nil) != !dryRun {
						t.Fatal("JSON hid failures or claimed preview changes were committed", report)
					}
					if partial, ok := report.Partial.(map[string]any); ok {
						if _, duplicate := partial["batch"]; duplicate {
							t.Fatal("partial result duplicated the top-level batch report")
						}
					}
					if err := decoder.Decode(new(any)); err != io.EOF {
						t.Fatal("more than one JSON document", err)
					}
					for _, row := range report.Batch.Results {
						if row.Session == first && (row.Error == nil || row.Error.Code != "inspection_test_failure" || len(row.Error.Next) != 1 ||
							!slices.Equal(row.Error.Next[0].Command, []string{"dbx", "--home", e.Store.Home, "status", first})) {
							t.Fatal("lost per-target guidance or home", row)
						}
					}
				} else {
					for _, text := range []string{first + ": failed", second.Session + ": succeeded", "Summary: 1 succeeded", "1 failed"} {
						if !strings.Contains(out.String(), text) {
							t.Fatal("missing shared batch report", text, out.String())
						}
					}
					if !strings.Contains(stderr.String(), "Cannot inspect selected runtime") {
						t.Fatal(stderr.String())
					}
				}
				if _, err := e.Store.Read(context.Background(), sessionRecord(t, e, first).ID); err != nil {
					t.Fatal("failed target lost state", err)
				}
				_, secondErr := e.Store.Read(context.Background(), second.SessionID)
				if (secondErr == nil) != dryRun {
					t.Fatal("wrong independent target outcome", secondErr)
				}
			})
		}
	}
}

func TestDeletionConfirmationUsesConfirmedRemovalCount(t *testing.T) {
	var out bytes.Buffer
	ui := cliui.New(context.Background(), strings.NewReader("y\nn\n"), &out)
	defer ui.Finish()
	confirmation := deletionConfirmation{ui: ui}
	if yes, err := confirmation.confirm(app.DeletePrompt{Containers: []string{"a", "b", "c"}}); err != nil || !yes {
		t.Fatal(yes, err)
	}
	if yes, err := confirmation.confirm(app.DeletePrompt{Sessions: []string{"session"}, RemovedContainers: []string{"a"}}); err != nil || yes {
		t.Fatal(yes, err)
	}
	if !strings.Contains(out.String(), "Container removed.") || strings.Contains(out.String(), "3 containers removed") {
		t.Fatal("confirmation confused approved removals with completed removals", out.String())
	}
}

func TestDeletionMenuPreviewShowsValidWorkAndFailures(t *testing.T) {
	f, out, q, first := frontendFixture(t, strings.NewReader(""))
	q.LocalName = "Second"
	second, err := f.e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	firstID := sessionRecord(t, f.e, first).Applied.ContainerID
	d := f.e.Docker.Runner.(*dockertest.Daemon)
	d.Fail = func(args []string) error {
		if slices.Equal(args, []string{"container", "inspect", firstID}) {
			return &docker.ExitError{Code: 42, Operation: "container"}
		}
		return nil
	}
	input := &choiceScript{t: t, out: out, steps: []string{"@Preview deletion", "@Back", "@Back"}}
	f.cmd.SetIn(input)
	f.m = testMenuCommand(f.cmd.Context(), input, out, f.cmd)
	if err := f.deleteWithOptions(app.DeleteOptions{Selection: app.Selection{All: true}, Scope: app.DeleteSession}); err != nil {
		t.Fatal(err, out.String())
	}
	for _, text := range []string{"Would delete", first + ": failed", second.Session + ": succeeded", "Summary:"} {
		if !strings.Contains(out.String(), text) {
			t.Fatal("preview hid valid work or failure", text, out.String())
		}
	}
	if _, err := f.e.Store.Read(context.Background(), second.SessionID); err != nil {
		t.Fatal("preview deleted state", err)
	}
}
