package migration

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func reportTerminalOutput(t *testing.T, render func(io.Writer) error) string {
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

func TestReportTerminalStylingKeepsContentAndPlainOutputs(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
	v := &Inventory{Items: []Item{
		{Key: "profile:a", Kind: "profile", Warnings: []string{"Retained state is not imported."}},
		{Key: "profile:b", Kind: "profile"},
		{Key: "session:a", Kind: "session", Issues: []string{"Workspace missing."}},
		{Key: "session:b", Kind: "session"},
	}}
	for _, report := range []struct {
		name   string
		render func(io.Writer, *Inventory, *Journal) error
	}{{"compact", CompactReport}, {"full", Report}} {
		t.Run(report.name, func(t *testing.T) {
			render := func(w io.Writer) error { return report.render(w, v, nil) }
			styled := reportTerminalOutput(t, render)
			requireText(t, styled, "\x1b[1mprofile:a\x1b[0m", "\x1b[1msession:a\x1b[0m", "\x1b[31m[Error]\x1b[0m", "\x1b[33m[Warning]\x1b[0m")
			var plain bytes.Buffer
			if err := render(&plain); err != nil {
				t.Fatal(err)
			}
			unstyle := strings.NewReplacer("\x1b[1m", "", "\x1b[2m", "", "\x1b[31m", "", "\x1b[32m", "", "\x1b[33m", "", "\x1b[36m", "", "\x1b[0m", "")
			if strings.Contains(plain.String(), "\x1b") || unstyle.Replace(styled) != plain.String() {
				t.Fatal("styling changed report content")
			}
			if report.name == "compact" {
				requireText(t, plain.String(), "\n\n  [Metadata OK] profile:b", "\n\n  [Metadata OK] session:b")
			}
			file, err := os.CreateTemp(t.TempDir(), "report-*")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := render(file); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(file.Name())
			if err != nil || string(data) != plain.String() {
				t.Fatal("redirected file contains styling or different content", err)
			}
			for _, mode := range []string{"NO_COLOR", "dumb"} {
				t.Run(mode, func(t *testing.T) {
					if mode == "NO_COLOR" {
						t.Setenv("NO_COLOR", "")
					} else {
						t.Setenv("TERM", "dumb")
					}
					if text := reportTerminalOutput(t, render); text != plain.String() {
						t.Fatal("disabled styling changed output")
					}
				})
			}
		})
	}
}

func TestInventoryItemLabelUsesPreviewStatusColors(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		state, code string
		item        Item
	}{
		{"Metadata OK", "36", Item{Key: "profile:ready", Kind: "profile"}},
		{"Warning", "33", Item{Key: "session:warning", Kind: "session", Warnings: []string{"Retained state."}}},
		{"Error", "31", Item{Key: "session:error", Kind: "session", Issues: []string{"Missing workspace."}}},
	} {
		t.Run(tc.state, func(t *testing.T) {
			text := reportTerminalOutput(t, func(w io.Writer) error { _, err := fmt.Fprintln(w, InventoryItemLabel(w, tc.item)); return err })
			requireText(t, text, "\x1b["+tc.code+"m["+tc.state+"]\x1b[0m", "\x1b[1m"+tc.item.Key+"\x1b[0m")
			var plain bytes.Buffer
			if label := InventoryItemLabel(&plain, tc.item); label != "["+tc.state+"] "+tc.item.Key {
				t.Fatal(label)
			}
		})
	}
}

func TestReportHeadingEscapesUntrustedContentBeforeStyling(t *testing.T) {
	paint := reportPaint{enabled: true}
	text := paint.item(Item{Kind: "session", Key: "session:bad\x1b[2J\nforged"})
	if strings.Contains(text, "\x1b[2J") || strings.Contains(text, "\n") {
		t.Fatal("untrusted heading injected terminal controls")
	}
	requireText(t, text, `bad\x1b[2J\nforged`, "\x1b[1m")
}
