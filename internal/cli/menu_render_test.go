package cli

import (
	"bufio"
	"bytes"
	"context"
	"devbox/internal/cliui"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"devbox/internal/app"
)

func TestMenuFrameFits(t *testing.T) {
	for _, tc := range []struct {
		frame string
		rows  int
		cols  int
		fits  bool
	}{
		{"Title\n1  First\nChoice > ", 5, 40, true},
		{"Title\n1  First\nChoice > ", 2, 40, false},
		{"Choice > ", 5, 9, false},
		{"\x1b[1mTitle\x1b[0m\nChoice > ", 5, 40, true},
		{"\x1b[HChoice > ", 5, 40, false},
		{"目录\nChoice > ", 5, 40, true},
		{"A〈\n> ", 4, 3, false}, // U+2329 occupies two cells.
		{"e\u0301\nChoice > ", 5, 40, false},
	} {
		if got := cliui.FrameFits(tc.frame, tc.rows, tc.cols); got != tc.fits {
			t.Errorf("cliui.FrameFits(%q, %d, %d) = %v; want %v", tc.frame, tc.rows, tc.cols, got, tc.fits)
		}
	}
}

func TestInteractiveMenuRedrawAndFallback(t *testing.T) {
	t.Setenv("TERM", "xterm")
	master, slave := testTerminal(t)
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 12, Col: 60}); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)
	cmd.SetContext(context.Background())
	m := newMenu(cmd)
	if !m.Redraws() || configDisplayWidth(m.Out) != 60 {
		t.Fatal("terminal rendering should preserve output width", configDisplayWidth(m.Out))
	}
	if _, err := master.WriteString("bad\n1\n0\n"); err != nil {
		t.Fatal(err)
	}
	choice, err := m.Select("Short menu", []string{"First"}, "Back")
	if err != nil || choice != 0 {
		t.Fatal(choice, err)
	}
	// This frame cannot fit in twelve rows. It must return to the ordinary
	// shell screen before printing the complete menu, then accept input.
	choices := make([]string, 15)
	for i := range choices {
		choices[i] = fmt.Sprintf("Item %d", i+1)
	}
	choice, err = m.Select("Long menu", choices, "Back")
	if err != nil || choice != -1 {
		t.Fatal(choice, err)
	}
	if err := m.Finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := slave.WriteString("\x00"); err != nil {
		t.Fatal(err)
	}
	text, err := bufio.NewReader(master).ReadString('\x00')
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(text, "\x1b[?1049h") != 1 || strings.Count(text, "\x1b[?1049l") != 1 || strings.Count(text, "Short menu") != 2 || !strings.Contains(text, "Long menu") || !strings.Contains(text, "Item 15") || !strings.Contains(text, "Choose 1–1") {
		t.Fatal("short menu did not redraw or long menu did not fall back", text)
	}
}

func TestEditUsesMenuScreenInTerminal(t *testing.T) {
	t.Setenv("TERM", "xterm")
	engine, request, _ := namedCLIFixture(t)
	master, slave := testTerminal(t)
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 80}); err != nil {
		t.Fatal(err)
	}
	if _, err := master.WriteString("2\n1\n0\n"); err != nil {
		t.Fatal(err)
	}
	name := ""
	cmd := editCommand(func(*cobra.Command) (*app.Engine, error) { return engine, nil }, &name)
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)
	cmd.SetArgs([]string{request.Workspace})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := slave.WriteString("\x00"); err != nil {
		t.Fatal(err)
	}
	text, err := bufio.NewReader(master).ReadString('\x00')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "\x1b[?1049h") || !strings.Contains(text, "\x1b[?1049l") || !strings.Contains(text, "Default session for ") {
		t.Fatal("edit did not render and close its interactive menu", text)
	}
	selected, err := engine.Store.ReadDefault(context.Background(), request.Workspace)
	if err != nil || selected == nil {
		t.Fatal("edit did not select the saved default", selected, err)
	}
}

func TestWarningRetryRemainsVisible(t *testing.T) {
	t.Setenv("TERM", "xterm")
	master, slave := testTerminal(t)
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 80}); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetContext(context.Background())
	m := newMenu(cmd)
	if _, err := master.WriteString("0\n0\n0\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Select("Initial menu", nil, "Back"); err != nil {
		t.Fatal(err)
	}
	if err := m.Pause(); err != nil {
		t.Fatal(err)
	}
	if _, err := slave.WriteString("Warning: check the config\n"); err != nil {
		t.Fatal(err)
	}
	m.PlainNext()
	if _, err := m.Select("Retry menu", nil, "Back"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Select("Next menu", nil, "Back"); err != nil {
		t.Fatal(err)
	}
	if err := m.Finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := slave.WriteString("\x00"); err != nil {
		t.Fatal(err)
	}
	text, err := bufio.NewReader(master).ReadString('\x00')
	if err != nil {
		t.Fatal(err)
	}
	warning := strings.Index(text, "Warning: check the config")
	retry := strings.Index(text, "Retry menu")
	if warning < 0 || retry < warning || strings.Contains(text[warning:retry], "\x1b[?1049h") || strings.Count(text, "\x1b[?1049h") != 2 {
		t.Fatal("warning and retry must stay on the shell screen", text)
	}
}

func TestMenuRestoresShellAfterCancellation(t *testing.T) {
	t.Setenv("TERM", "xterm")
	master, slave := testTerminal(t)
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 80}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := &cobra.Command{}
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetContext(ctx)
	m := newMenu(cmd)
	done := make(chan error, 1)
	go func() {
		_, err := m.Select("Waiting menu", []string{"First"}, "Back")
		done <- err
	}()
	if err := master.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(master)
	output, err := reader.ReadBytes('>')
	if err != nil || !bytes.Contains(output, []byte("\x1b[?1049h")) {
		t.Fatal("menu did not enter temporary screen", err, string(output))
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled read returned without an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled read did not stop")
	}
	if err := m.Finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := slave.WriteString("\x00"); err != nil {
		t.Fatal(err)
	}
	rest, err := reader.ReadString('\x00')
	if err != nil || !strings.Contains(rest, "\x1b[?1049l") {
		t.Fatal("shell screen was not restored", err, rest)
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
		t.Fatal("redirected output must not use terminal escapes")
	}
	if _, err := m.Select("Plain menu", []string{"First"}, "Back"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b[") || !strings.Contains(output.String(), "Plain menu") {
		t.Fatal(output.String())
	}
}

func TestMenuFinishedOutputIsNotHidden(t *testing.T) {
	t.Setenv("TERM", "xterm")
	master, slave := testTerminal(t)
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 80}); err != nil {
		t.Fatal(err)
	}
	if _, err := master.WriteString("0\n"); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetContext(context.Background())
	m := newMenu(cmd)
	if _, err := m.Select("Menu", nil, "Back"); err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(m.Out, "Saved.\n")
	if err := m.Finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := slave.WriteString("\x00"); err != nil {
		t.Fatal(err)
	}
	text, err := bufio.NewReader(master).ReadString('\x00')
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(text, "\x1b[?1049l") < 0 || strings.Index(text, "Saved.") < strings.Index(text, "\x1b[?1049l") {
		t.Fatal("final message remained hidden in the alternate screen", text)
	}
}
