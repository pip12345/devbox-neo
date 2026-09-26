package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"devbox/internal/app"
	"devbox/internal/harness"
	"devbox/internal/resource"
	"devbox/internal/store"
	"golang.org/x/sys/unix"
)

func testTerminal(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("pseudo-terminal unavailable: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slave.Close() })
	return master, slave
}

func terminalOutput(t *testing.T, width int, render func(*os.File) error) string {
	t.Helper()
	master, slave := testTerminal(t)
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: uint16(width)}); err != nil {
		t.Fatal(err)
	}
	if err := render(slave); err != nil {
		t.Fatal(err)
	}
	if _, err := slave.WriteString("\x00"); err != nil {
		t.Fatal(err)
	}
	text, err := bufio.NewReader(master).ReadString('\x00')
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(strings.TrimSuffix(text, "\x00"), "\r\n", "\n")
}

func enableTerminalColors(t *testing.T) {
	t.Helper()
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
}

func unstyle(text string) string {
	return strings.NewReplacer("\x1b[1m", "", "\x1b[2m", "", "\x1b[32m", "", "\x1b[0m", "").Replace(text)
}

func TestListDimsOnlyInactiveRowsWithoutChangingAlignment(t *testing.T) {
	enableTerminalColors(t)
	now := time.Now()
	views := []app.View{
		{Name: "active", Exists: true, Running: true, Workspace: "/work/a"},
		{Name: "inactive-long-name", Exists: true, Workspace: "/work/b", Error: "broken record"},
		{Name: "missing", Workspace: "/work/c", Pending: &store.Reservation{Mode: "clone", Phase: "prepare", Source: "a", Destination: "b"}},
	}
	renderers := []func(io.Writer) error{
		func(out io.Writer) error { return printSessionList(out, views, false, now) },
		func(out io.Writer) error { return printSessionList(out, views, true, now) },
	}
	for _, render := range renderers {
		styled := terminalOutput(t, 80, func(out *os.File) error { return render(out) })
		lines := strings.Split(styled, "\n")
		for i, line := range lines {
			plainLine := unstyle(line)
			shouldDim := strings.HasPrefix(plainLine, "/work/b ") || strings.HasPrefix(plainLine, "/work/c ")
			if strings.Contains(line, "\x1b[2m") != shouldDim {
				t.Fatalf("unexpected styling on line %d: %q", i, line)
			}
		}
		var plain bytes.Buffer
		if err := render(&plain); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(plain.String(), "\x1b") || unstyle(styled) != plain.String() {
			t.Fatalf("styling changed list content or alignment:\nstyled=%q\nplain=%q", styled, plain.String())
		}
	}
	for _, mode := range []string{"NO_COLOR", "dumb"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "NO_COLOR" {
				t.Setenv("NO_COLOR", "")
			} else {
				t.Setenv("TERM", "dumb")
			}
			for _, render := range renderers {
				text := terminalOutput(t, 80, func(out *os.File) error { return render(out) })
				if strings.Contains(text, "\x1b") {
					t.Fatal("disabled styling emitted ANSI", text)
				}
			}
		})
	}
}

func TestMenusSharePresentationAndPreserveSelections(t *testing.T) {
	enableTerminalColors(t)
	choices := []string{"pi", "opencode"}
	for _, kind := range []string{"config", "init-one", "init-many"} {
		t.Run(kind, func(t *testing.T) {
			render := func(out io.Writer) error {
				reader := bufio.NewReader(strings.NewReader("2\n"))
				switch kind {
				case "config":
					m := testMenu(context.Background(), reader, out)
					selection, err := m.Select("Harness", choices, "Back")
					if err == nil && selection != 1 {
						t.Fatal("config choice changed", selection)
					}
					return err
				case "init-one":
					m := testMenu(context.Background(), reader, out)
					selection, err := m.SelectCurrent("Harness", choices, 0, "", "Cancel")
					if err == nil && selection != 1 {
						t.Fatal("harness choice changed", selection)
					}
					return err
				default:
					m := testMenu(context.Background(), bufio.NewReader(strings.NewReader("2\n5\n")), out)
					options, proceed, err := optionalFilesMenu(m, t.TempDir(), resource.SetupOptions{})
					if err == nil && (!proceed || len(options.Artifacts) != 1 || options.Artifacts[0] != "setup.sh") {
						t.Fatal("artifact toggle changed", options)
					}
					return err
				}
			}
			text := terminalOutput(t, 80, func(out *os.File) error { return render(out) })
			if !strings.Contains(text, "\x1b[1m") || !strings.Contains(text, "[1]") || !strings.Contains(text, menuChoicePrompt) {
				t.Fatal("menu did not share title/choice styling", text)
			}
			if kind == "init-many" && (!strings.Contains(text, "\x1b[32m✓") || strings.Contains(text, "comma-separated")) {
				t.Fatal("artifact choices did not use styled single-number toggles", text)
			}
			if kind == "init-one" && !strings.Contains(text, "\x1b[32m(selected)") {
				t.Fatal("current selection marker was not styled", text)
			}
			var plain bytes.Buffer
			if err := render(&plain); err != nil {
				t.Fatal(err)
			}
			if unstyle(text) != plain.String() || strings.Contains(plain.String(), "\x1b") {
				t.Fatal("plain and styled menu contents differ")
			}
		})
	}
}

func TestConfigCreationUsesStyledMenusForNamesAndPaths(t *testing.T) {
	enableTerminalColors(t)
	for _, name := range []string{"claude", "opencode", "pi"} {
		for _, kind := range []string{"named", "path"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				home := t.TempDir()
				registry, err := harness.Enumerate(home)
				if err != nil {
					t.Fatal(err)
				}
				target, selected := "basic", -1
				for i, entry := range registry.Valid {
					if entry.Definition.Name == name {
						selected = i
					}
				}
				if selected < 0 {
					t.Fatal("built-in harness missing from setup", name)
				}
				configPath := filepath.Join(home, "configs/basic/config.json")
				if kind == "path" {
					target = filepath.Join(t.TempDir(), "config")
					configPath = filepath.Join(target, "config.json")
				}
				p := newTerminalProbe(t)
				before, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
				if err != nil {
					t.Fatal(err)
				}
				done := p.workflow(func(ctx context.Context, tty *os.File) error {
					cmd := New()
					cmd.SetIn(tty)
					cmd.SetOut(tty)
					cmd.SetErr(tty)
					cmd.SetArgs([]string{"--home", home, "config", "create", target})
					return cmd.ExecuteContext(ctx)
				})
				p.wait("Name/location")
				p.send("\x1b[B\r")
				p.wait("Leave unset")
				p.send("\x1b[H" + strings.Repeat("\x1b[B", selected) + "\r")
				p.send("\x1b[F\r")
				p.finish(done)
				text := p.output()
				if !strings.Contains(text, "\x1b[?1049h") || !strings.Contains(text, "\x1b[?1049l") {
					t.Fatal("creation did not use and restore native UI", text)
				}
				data, err := os.ReadFile(configPath)
				if err != nil || !strings.Contains(string(data), `"`+name+`"`) {
					t.Fatal("styled init did not save selected harness", string(data), err)
				}
				after, err := unix.IoctlGetTermios(int(p.slave.Fd()), unix.TCGETS)
				if err != nil || *before != *after {
					t.Fatal("init changed terminal mode", err)
				}
			})
		}
	}
}

func TestSharedMenuWrapping(t *testing.T) {
	enableTerminalColors(t)
	choice := strings.Repeat("long-value-", 8)
	text := terminalOutput(t, 32, func(out *os.File) error {
		m := testMenu(context.Background(), strings.NewReader("0\n"), out)
		_, err := m.Select("A longer menu heading that must wrap", []string{choice}, "Back")
		return err
	})
	for _, line := range strings.Split(unstyle(text), "\n") {
		if utf8.RuneCountInString(line) > 32 {
			t.Fatalf("menu exceeded terminal width: %q", line)
		}
	}
	if !strings.Contains(text, "\x1b[1m") || !strings.Contains(text, "   [1]  long-value-") {
		t.Fatal("wrapped menu lost styling or numbering", text)
	}
}
