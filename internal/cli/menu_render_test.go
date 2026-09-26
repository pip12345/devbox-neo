package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"devbox/internal/app"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func TestInteractiveMenusShareOneTerminalAndRestoreIt(t *testing.T) {
	p := newTerminalProbe(t)
	before, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	done := p.workflow(func(ctx context.Context, tty *os.File) (err error) {
		cmd := &cobra.Command{}
		cmd.SetContext(ctx)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		m := newMenu(cmd)
		defer func() { err = errors.Join(err, m.Finish()) }()
		if !m.Redraws() {
			return fmt.Errorf("terminal UI not enabled")
		}
		choice, err := m.Select("First screen", []string{"Alpha action", "Beta action"}, "Back")
		if err != nil {
			return err
		}
		if choice != 1 {
			return fmt.Errorf("wrong selected action: %d", choice)
		}
		choices := make([]string, 30)
		for i := range choices {
			choices[i] = fmt.Sprintf("Long action %02d", i)
		}
		choice, err = m.Select("Scrollable screen", choices, "Back")
		if err != nil {
			return err
		}
		if choice != 29 {
			return fmt.Errorf("long list did not select final item")
		}
		m.Receipt("Saved receipt.")
		return nil
	})
	p.wait("Alpha action")
	p.send("\x1b[B\r")
	p.wait("Long action")
	p.send("\x1b[F")
	p.send("\r")
	p.finish(done)
	text := p.output()
	if strings.Count(text, "\x1b[?1049h") != 1 || strings.Count(text, "\x1b[?1049l") != 1 {
		t.Fatal("nested screens restarted the terminal", text)
	}
	if strings.Index(text, "Saved receipt.") < strings.Index(text, "\x1b[?1049l") {
		t.Fatal("receipt hidden in alternate screen", text)
	}
	after, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
	if err != nil || *before != *after {
		t.Fatal("terminal mode not restored", err)
	}
}
func TestEditUsesNativeMenuAndSavesDefault(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	p := newTerminalProbe(t)
	done := p.workflow(func(ctx context.Context, tty *os.File) error {
		name := ""
		cmd := editCommand(func(*cobra.Command) (*app.Engine, error) { return e, nil }, &name)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		cmd.SetErr(tty)
		cmd.SetArgs([]string{q.Workspace})
		return cmd.ExecuteContext(ctx)
	})
	p.wait("Application actions")
	p.send("\x1b[B\r")
	p.wait("Session · Main")
	p.send("/Make folder\r\r")
	// The saved-state reload may outlast a fixed inter-key delay. Wait for
	// the refreshed parent controls before sending its exit key.
	p.wait("Clear folder default")
	p.send("q")
	p.wait("Application actions")
	p.send("q")
	p.finish(done)
	selected, err := e.Store.ReadDefault(context.Background(), q.Workspace)
	if err != nil || selected == nil {
		t.Fatal("default not saved", selected, err, p.output())
	}
	if !strings.Contains(p.output(), "Default session for ") {
		t.Fatal("default receipt missing", p.output())
	}
}
func TestWarningsStayVisibleUntilAcknowledged(t *testing.T) {
	p := newTerminalProbe(t)
	done := p.workflow(func(ctx context.Context, tty *os.File) (err error) {
		cmd := &cobra.Command{}
		cmd.SetContext(ctx)
		cmd.SetIn(tty)
		cmd.SetOut(tty)
		m := newMenu(cmd)
		defer func() { err = errors.Join(err, m.Finish()) }()
		if _, err = m.Select("Initial screen", nil, "Back"); err != nil {
			return err
		}
		if err = m.Pause(); err != nil {
			return err
		}
		fmt.Fprintln(tty, "Warning: review this before proceeding")
		if err = m.ReviewOutput(); err != nil {
			return err
		}
		_, err = m.Select("Retry screen", nil, "Back")
		return err
	})
	p.wait("Initial screen")
	p.send("\r")
	p.wait("Press Enter")
	text := p.output()
	warning := strings.Index(text, "Warning: review")
	if warning < 0 || strings.Contains(text[warning:], "\x1b[?1049h") {
		t.Fatal("warning hidden before acknowledgement", text)
	}
	p.send("\n")
	p.wait("Retry")
	p.send("\r")
	p.finish(done)
}
func TestMenuCancellationRestoresTerminal(t *testing.T) {
	p := newTerminalProbe(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		cmd := &cobra.Command{}
		cmd.SetContext(ctx)
		cmd.SetIn(p.slave)
		cmd.SetOut(p.slave)
		m := newMenu(cmd)
		_, err := m.Select("Waiting", []string{"Cancel me"}, "Back")
		done <- errors.Join(err, m.Finish())
	}()
	p.wait("Cancel me")
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled menu did not stop")
	}
	p.drain()
	if !strings.Contains(p.output(), "\x1b[?1049l") {
		t.Fatal("alternate screen not restored", p.output())
	}
}
func TestMenuKeepsRedirectedOutputPlain(t *testing.T) {
	master, slave := testTerminal(t)
	if _, err := master.WriteString("0\n"); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	var output bytes.Buffer
	cmd.SetIn(slave)
	cmd.SetOut(&output)
	cmd.SetContext(context.Background())
	m := newMenu(cmd)
	if m.Redraws() {
		t.Fatal("redirected output enabled terminal UI")
	}
	if _, err := m.Select("Plain menu", []string{"First"}, "Back"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b[") || !strings.Contains(output.String(), "Plain menu") {
		t.Fatal(output.String())
	}
}
