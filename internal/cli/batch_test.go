package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/docker/dockertest"
	"github.com/spf13/cobra"
)

func TestBulkNetworkSelectionRejectsConflictsBeforeInitialization(t *testing.T) {
	for _, args := range [][]string{
		{"inspect"}, {"inspect", "--all", "."}, {"inspect", "--all", "--name", "Main"},
		{"connect", "mcp"}, {"connect", "mcp", ".", "--all"}, {"disconnect", "mcp", "--all", "--name", "Main"},
	} {
		localName := ""
		cmd := networkCommands(func(*cobra.Command) (*app.Engine, error) {
			t.Fatal("invalid network selection initialized the engine", args)
			return nil, nil
		}, &localName)
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatal("invalid selection accepted", args)
		}
	}
}

func TestBulkNetworkCLIPresentsPartialWorkAndPreservesExitStatus(t *testing.T) {
	e, q, first := namedCLIFixture(t)
	ctx := context.Background()
	q.LocalName = "Second"
	second, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(q.Sources[0].Path, "config.json"), []byte(`{"version":1,"harness":"pi","network":"host"}`), 0600); err != nil {
		t.Fatal(err)
	}
	q.LocalName = "Host"
	host, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	d := e.Docker.Runner.(*dockertest.Daemon)
	failure := &docker.ExitError{Code: 27, Operation: "network"}
	cause := commanderror.New("network_test_failure", "Cannot attach this session.", first, failure,
		commanderror.Next("Inspect session", "status", first))
	firstID := sessionRecord(t, e, first).Applied.ContainerID
	d.Fail = func(args []string) error {
		if slices.Equal(args, []string{"network", "connect", "mcp", firstID}) {
			return cause
		}
		return nil
	}
	root := &cobra.Command{Use: "dbx", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().String("home", "", "")
	localName := ""
	root.AddCommand(networkCommands(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &localName))
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--home", e.Store.Home, "network", "connect", "mcp", "--all"})
	if code := Execute(ctx, root); code != 27 {
		t.Fatal("batch lost Docker exit status", code, out.String(), stderr.String())
	}
	for _, text := range []string{first + ": failed", second.Session + ": succeeded", host.Session + ": skipped", "1 succeeded, 0 unchanged, 1 skipped, 1 failed"} {
		if !strings.Contains(out.String(), text) {
			t.Fatal("missing per-session result", text, out.String())
		}
	}
	if !strings.Contains(stderr.String(), "Cannot attach this session") || !strings.Contains(stderr.String(), "--home "+shellQuote(e.Store.Home)+" status "+first) {
		t.Fatal("batch lost error details or home-scoped recovery", stderr.String())
	}
	if got := sessionRecord(t, e, second.SessionID).Settings.SecondaryNetworks; !slices.Equal(got, []string{"mcp"}) {
		t.Fatal("CLI failed to retain successful work", got)
	}
}

func TestBulkNetworkInspectionJSONRetainsFactsAndFailures(t *testing.T) {
	e, q, first := namedCLIFixture(t)
	ctx := context.Background()
	q.LocalName = "Second"
	second, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	d := e.Docker.Runner.(*dockertest.Daemon)
	failure := &docker.ExitError{Code: 29, Operation: "container"}
	firstID := sessionRecord(t, e, first).Applied.ContainerID
	d.Fail = func(args []string) error {
		if slices.Equal(args, []string{"container", "inspect", firstID}) {
			return failure
		}
		return nil
	}
	root := &cobra.Command{Use: "dbx", SilenceErrors: true, SilenceUsage: true}
	localName := ""
	root.AddCommand(networkCommands(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &localName))
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	root.SetArgs([]string{"network", "inspect", "--all"})
	if code := Execute(ctx, root); code != 29 {
		t.Fatal(code, stderr.String())
	}
	var report batchOutput
	decoder := json.NewDecoder(&out)
	if err := decoder.Decode(&report); err != nil || len(report.Results) != 2 || report.Summary.Failed != 1 || report.Summary.Succeeded != 1 {
		t.Fatal(report, err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		t.Fatal("inspection emitted more than one JSON document", err)
	}
	for _, row := range report.Results {
		if row.Session == first && row.Error == nil || row.Session == second.Session && row.Result == nil {
			t.Fatal("inspection dropped partial facts or error", row)
		}
	}
}

func TestBulkRecreateCLIReportsSuccessAlongsideFailure(t *testing.T) {
	e, q, first := namedCLIFixture(t)
	q.LocalName = "Second"
	second, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	beforeSecond := sessionRecord(t, e, second.SessionID).Applied.ContainerID
	firstID := sessionRecord(t, e, first).Applied.ContainerID
	d := e.Docker.Runner.(*dockertest.Daemon)
	cause := errors.New("cannot remove this container")
	d.Fail = func(args []string) error {
		if slices.Equal(args, []string{"rm", firstID}) {
			return cause
		}
		return nil
	}
	localName := ""
	cmd := recreateCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &localName)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"--all", "--container"})
	if err := cmd.Execute(); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), first+": failed") || !strings.Contains(out.String(), second.Session+": succeeded") || !strings.Contains(out.String(), "Summary:") {
		t.Fatal("bulk recreation hid partial results", out.String())
	}
	if sessionRecord(t, e, second.SessionID).Applied.ContainerID == beforeSecond {
		t.Fatal("CLI stopped recreating after another session failed")
	}
}

func TestAllSessionNetworkMenuPlacementAndScope(t *testing.T) {
	f, out, _, _ := frontendFixture(t, strings.NewReader("3\n0\n0\n"))
	if err := f.allSessions(); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Networks for all sessions", "Networks for all current sessions", "Inspect networks", "Connect network", "Disconnect network"} {
		if !strings.Contains(out.String(), text) {
			t.Fatal("missing bulk network UI", text, out.String())
		}
	}
	if strings.Contains(out.String(), "Environment exports") {
		t.Fatal("bulk network UI offered ambiguous shared shell exports")
	}
}

func TestBulkNetworkMenuUsesSharedBatchResults(t *testing.T) {
	f, out, q, first := frontendFixture(t, strings.NewReader("mcp\n"))
	q.LocalName = "Second"
	second, err := f.e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := f.networkChange("", true)
	if err != nil || !changed {
		t.Fatal(changed, err, out.String())
	}
	for _, name := range []string{first, second.Session} {
		if !slices.Equal(sessionRecord(t, f.e, name).Settings.SecondaryNetworks, []string{"mcp"}) || !strings.Contains(out.String(), name+": succeeded") {
			t.Fatal("menu did not use shared bulk network operation", name, out.String())
		}
	}
	if !strings.Contains(out.String(), "all current sessions") || !strings.Contains(out.String(), "Summary: 2 succeeded") {
		t.Fatal("menu hid bulk scope or outcomes", out.String())
	}
}

func TestBatchPresentationFailurePreservesOperationCause(t *testing.T) {
	cause := &docker.ExitError{Code: 33, Operation: "network"}
	operationErr := &app.BatchError{Causes: []error{cause}}
	report := app.BatchReport[app.NetworkFacts]{Results: []app.BatchResult[app.NetworkFacts]{{Session: "session", Status: app.BatchFailed, Err: cause}}}
	cmd := &cobra.Command{Use: "dbx"}
	cmd.SetOut(diagnosticWriter(func([]byte) (int, error) { return 0, io.ErrClosedPipe }))
	cmd.SetErr(io.Discard)
	err := presentBatch(cmd, report, operationErr, false)
	if !errors.Is(err, cause) || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("presentation failure hid the operation error", err)
	}
}
