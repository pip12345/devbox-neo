package cli

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestConfigCommandWithTerminalInputKeepsTerminalMode(t *testing.T) {
	master, slave := testTerminal(t)
	for _, size := range []struct{ columns, want int }{{52, 52}, {120, 80}} {
		if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: uint16(size.columns)}); err != nil {
			t.Fatal(err)
		}
		if got := configDisplayWidth(slave); got != size.want {
			t.Fatalf("display width = %d, want %d", got, size.want)
		}
	}
	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NO_COLOR", "")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERM", "xterm")
	if !terminalColors(slave).enabled || terminalColors(&bytes.Buffer{}).enabled {
		t.Fatal("color must be limited to terminal output")
	}
	t.Setenv("NO_COLOR", "1")
	if terminalColors(slave).enabled {
		t.Fatal("NO_COLOR was ignored")
	}
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERM", "dumb")
	if terminalColors(slave).enabled {
		t.Fatal("dumb terminal received styling")
	}
	s := menuService(t)
	input := fieldNumber(t, "global", "ignore_project_overrides") + "\n1\n1\n0\n"
	if _, err = master.WriteString(input); err != nil {
		t.Fatal(err)
	}
	cmd := New()
	cmd.SetIn(slave)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", s.Home, "global", "config"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(out.String(), err)
	}
	owner, _ := s.ConfigOwner("global", "")
	source, err := s.ConfigSource(owner)
	if err != nil || string(source["ignore_project_overrides"]) != "true" {
		t.Fatal("terminal menu did not save", out.String(), err)
	}
	after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || *before != *after {
		t.Fatal("menu changed terminal mode", err)
	}
}
