package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"devbox/internal/docker/dockertest"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

func statusInventoryCalls(d *dockertest.Daemon) int {
	count := 0
	for _, call := range d.Calls {
		if len(call) > 1 && call[0] == "image" && call[1] == "ls" {
			count++
		}
	}
	return count
}

func TestNativePaneNavigationDoesNotQueryDocker(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	t.Chdir(q.Workspace)
	var calls atomic.Int32
	e.Docker.Runner.(*dockertest.Daemon).Fail = func([]string) error {
		calls.Add(1)
		return nil
	}
	p := newTerminalProbe(t)
	done := p.workflow(func(ctx context.Context, tty *os.File) (err error) {
		cmd := &cobra.Command{Use: "dbx"}
		cmd.SetContext(ctx)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		cmd.SetErr(tty)
		m := newMenu(cmd)
		defer func() { err = errors.Join(err, m.Finish()) }()
		f := frontend{m: m, cmd: cmd, e: e, s: &resource.Service{Home: e.Store.Home}}
		return f.browse(false)
	})
	p.wait("Last active")
	initial := calls.Load()
	for range 2 {
		p.send("\x1b[C")
		p.wait("Resume the harness conversation")
		p.send("\x1b[D")
		p.wait("Enter/→ actions")
		if got := calls.Load(); got != initial {
			t.Fatalf("pane navigation queried Docker: calls %d, want %d", got, initial)
		}
	}
	p.send("q")
	p.finish(done)
}

func TestFolderPaneNavigationReusesStatus(t *testing.T) {
	f, out, _, _ := frontendFixture(t, strings.NewReader("1\n0\n1\n0\n0\n"))
	d := f.e.Docker.Runner.(*dockertest.Daemon)
	d.Calls = nil
	if err := f.browse(false); err != nil {
		t.Fatal(err, out.String())
	}
	if calls := statusInventoryCalls(d); calls != 1 {
		t.Fatal("folder navigation reran status", calls)
	}
}

func TestDefaultChangesInvalidateSessionSnapshot(t *testing.T) {
	f, _, _, _ := frontendFixture(t, strings.NewReader(""))
	d := f.e.Docker.Runner.(*dockertest.Daemon)
	d.Calls = nil
	report, err := f.sessionReport()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.defaultAction(report.Sessions[0]).Run(); err != nil {
		t.Fatal(err)
	}
	report, err = f.sessionReport()
	if err != nil || !report.Sessions[0].Default {
		t.Fatal("default save left stale snapshot", report, err)
	}
	if _, err := f.defaultAction(report.Sessions[0]).Run(); err != nil {
		t.Fatal(err)
	}
	report, err = f.sessionReport()
	if err != nil || report.Sessions[0].Default {
		t.Fatal("default clear left stale snapshot", report, err)
	}
	if calls := statusInventoryCalls(d); calls != 3 {
		t.Fatal("expected initial check and one check per saved change", calls)
	}
}

func TestFailedSnapshotRefreshRetainsInventoryWithoutNavigationRetries(t *testing.T) {
	f, _, _, _ := frontendFixture(t, strings.NewReader(""))
	initial, err := f.sessionReport()
	if err != nil {
		t.Fatal(err)
	}
	d := f.e.Docker.Runner.(*dockertest.Daemon)
	d.Fail = func(args []string) error {
		return errors.New("Docker unavailable")
	}
	f.invalidateSessions()
	report, err := f.sessionReport()
	if err == nil || len(report.Sessions) != len(initial.Sessions) || report.Sessions[0].Target != initial.Sessions[0].Target {
		t.Fatal("failed refresh removed displayed sessions", report, err)
	}
	calls := len(d.Calls)
	if _, err := f.sessionReport(); err == nil || len(d.Calls) != calls {
		t.Fatal("navigation retried a failed status check", err, len(d.Calls), calls)
	}
}

func TestPaneNavigationReusesStatusUntilExplicitRefresh(t *testing.T) {
	input := &workflowInput{lines: []string{"2\n", "0\n", "7\n", "2\n", "0\n", "0\n"}}
	f, out, _, _ := frontendFixture(t, input)
	d := f.e.Docker.Runner.(*dockertest.Daemon)
	d.Calls = nil
	input.before = func(step int) {
		want := 1
		if step >= 3 {
			want = 2
		}
		if calls := statusInventoryCalls(d); calls != want {
			t.Fatalf("pane navigation reran status: step %d, calls %d, want %d", step, calls, want)
		}
		if step == 0 {
			if err := os.WriteFile(filepath.Join(f.s.Home, "configs/base/config.json"), []byte(`{"version":1,"harness":"pi","network":"host"}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if step == 3 && !strings.Contains(out.String(), "Main ↻") {
			t.Fatal("explicit Refresh did not update pending changes", out.String())
		}
	}
	if err := f.browse(false); err != nil {
		t.Fatal(err, out.String())
	}
}
