package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/app"
	"devbox/internal/cliui"
	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

func frontendFixture(t *testing.T, input io.Reader) (*frontend, *bytes.Buffer, app.CreateRequest, string) {
	t.Helper()
	e, q, name := namedCLIFixture(t)
	out := new(bytes.Buffer)
	cmd := &cobra.Command{Use: "dbx"}
	cmd.SetContext(context.Background())
	cmd.SetIn(input)
	cmd.SetOut(out)
	cmd.SetErr(out)
	f := &frontend{m: testMenuCommand(cmd.Context(), input, out, cmd), cmd: cmd, e: e, s: &resource.Service{Home: e.Store.Home}, engine: func(*cobra.Command) (*app.Engine, error) { return e, nil }}
	return f, out, q, name
}
func TestConfigBrowserCreatesWithoutDockerOrSelectingSession(t *testing.T) {
	f, out, _, name := frontendFixture(t, strings.NewReader("2\n1\nfrom-browser\n4\n0\n"))
	savedPath := filepath.Join(f.s.Home, "sessions", sessionRecord(t, f.e, name).Directory, "session.json")
	f.e = nil
	f.engine = func(*cobra.Command) (*app.Engine, error) {
		t.Fatal("config browser initialized Docker engine")
		return nil, nil
	}
	before, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.browse(true); err != nil {
		t.Fatal(err, out.String())
	}
	if _, err := os.Stat(filepath.Join(f.s.Home, "configs", "from-browser", "config.json")); err != nil {
		t.Fatal(err, out.String())
	}
	after, err := os.ReadFile(savedPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("independent config creation changed a session", err)
	}
}
func TestSessionBrowserDispatchesRealLifetimeOperations(t *testing.T) {
	input := &workflowInput{lines: []string{"10\n", "11\n", "2\n", "0\n"}}
	f, out, _, name := frontendFixture(t, input)
	input.before = func(step int) {
		if step != 1 && step != 3 {
			return
		}
		r, err := f.e.Locate(context.Background(), name, "")
		if err != nil || r.Settings.ManualStart != (step == 1) {
			t.Fatal("UI did not persist actual lifetime intent", step, r.Settings.ManualStart, err)
		}
	}
	if err := f.session(app.View{Target: name}); err != nil {
		t.Fatal(err, out.String())
	}
	r, err := f.e.Locate(context.Background(), name, "")
	if err != nil || r.Settings.ManualStart || r.Action != "stop" {
		t.Fatal(r, err)
	}
}
func TestFrontendArgumentsPreserveExactArgv(t *testing.T) {
	f, out, _, _ := frontendFixture(t, strings.NewReader("1\nhello world\n2\n\n3\n--flag\n0\n"))
	var args []string
	if err := f.arguments("Arguments", &args); err != nil {
		t.Fatal(err, out.String())
	}
	if !reflect.DeepEqual(args, []string{"hello world", "", "--flag"}) {
		t.Fatal("argv was shell-split or empty argument dropped", args)
	}
}
func TestFrontendCopyAndMoveUseDurableTransfer(t *testing.T) {
	for _, move := range []bool{false, true} {
		input := "2\nSecond\n"
		if move {
			input += "3\n"
		}
		input += "4\n0\n5\ny\n"
		f, out, q, name := frontendFixture(t, strings.NewReader(input))
		before, err := f.e.Locate(context.Background(), name, "")
		if err != nil {
			t.Fatal(err)
		}
		moved, err := f.transfer(name)
		if err != nil || moved != move {
			t.Fatal(moved, err, out.String())
		}
		record, err := f.e.Locate(context.Background(), q.Workspace, "Second")
		if err != nil {
			t.Fatal(err, out.String())
		}
		if (record.ID == before.ID) != move {
			t.Fatal("transfer lineage changed", record.ID, before.ID)
		}
		_, err = f.e.Store.Read(context.Background(), before.Directory)
		if os.IsNotExist(err) != move {
			t.Fatal("source retention changed", move, err)
		}
	}
}
func TestFrontendDeletionRetainsHistoryWhenDeclined(t *testing.T) {
	f, out, _, name := frontendFixture(t, strings.NewReader("1\n2\n4\ny\nn\n"))
	before, err := f.e.Locate(context.Background(), name, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.delete([]string{name}); err != nil {
		t.Fatal(err, out.String())
	}
	after, err := f.e.Locate(context.Background(), name, "")
	if err != nil || before.ID != after.ID {
		t.Fatal("declined history deletion removed state", err)
	}
	report, err := f.e.List(context.Background(), "")
	if err != nil || len(report.Sessions) != 1 || report.Sessions[0].Exists {
		t.Fatal("container was not removed", report, err)
	}
	if !strings.Contains(out.String(), "Container removed.") || !strings.Contains(out.String(), "Delete saved data and history?") {
		t.Fatal("two-stage deletion contract was lost", out.String())
	}
}
func TestForegroundScopeCancelsOperationNotMenu(t *testing.T) {
	root, closeSignals := SignalContext(context.Background())
	defer closeSignals()
	ctx, done := operationContext(root)
	state := root.Value(interruptKey{}).(*interrupts)
	state.mu.Lock()
	state.operation()
	state.mu.Unlock()
	if !errors.Is(ctx.Err(), context.Canceled) || root.Err() != nil {
		t.Fatal("operation cancellation killed menu context")
	}
	done()
	state.mu.Lock()
	active := state.operation != nil
	state.mu.Unlock()
	if active {
		t.Fatal("signal routing retained a finished operation")
	}
	next, finish := operationContext(root)
	defer finish()
	if next.Err() != nil {
		t.Fatal("cancelled context reused for next operation")
	}
}
func TestBareBrowsersKeepNonTerminalHelpStateless(t *testing.T) {
	for _, args := range [][]string{nil, {"config"}} {
		home := filepath.Join(t.TempDir(), "unopened")
		cmd := New()
		var out bytes.Buffer
		cmd.SetIn(strings.NewReader(""))
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append([]string{"--home", home}, args...))
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatal(err, out.String())
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Fatal("bare non-terminal entry did not show help", out.String())
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatal("help initialized state", err)
		}
	}
}
func TestFrontendJSONArgumentsPreserveControlCharacters(t *testing.T) {
	f, out, _, _ := frontendFixture(t, strings.NewReader("2\n[\"line\\nnext\",\"\",\":back\"]\n0\n"))
	var args []string
	if err := f.arguments("Arguments", &args); err != nil {
		t.Fatal(err, out.String())
	}
	if !reflect.DeepEqual(args, []string{"line\nnext", "", ":back"}) {
		t.Fatal("JSON argv changed values", args)
	}
}

func TestForegroundFailureReturnsToUsableMenu(t *testing.T) {
	f, out, _, _ := frontendFixture(t, strings.NewReader("0\n"))
	failure := errors.New("operation failed")
	if err := f.foreground("Example", func(context.Context) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if f.m.Context.Err() != nil || !strings.Contains(out.String(), "operation failed") {
		t.Fatal("operation error killed menu or hid result", out.String())
	}
	if err := f.form("Still usable", func() []cliui.Action { return nil }); err != nil {
		t.Fatal(err)
	}
}
