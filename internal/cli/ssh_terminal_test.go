package cli

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSSHMessagesUseTerminalOutputMode(t *testing.T) {
	for _, mode := range []string{"cooked", "raw", "no-onlcr"} {
		t.Run(mode, func(t *testing.T) {
			output := terminalOutput(t, 80, func(file *os.File) error {
				state, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
				if err != nil {
					return err
				}
				switch mode {
				case "raw":
					state.Oflag &^= unix.OPOST
				case "no-onlcr":
					state.Oflag |= unix.OPOST
					state.Oflag &^= unix.ONLCR
				default:
					state.Oflag |= unix.OPOST | unix.ONLCR
				}
				if err := unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, state); err != nil {
					return err
				}
				sshTerminalMessage(file, "\nConnected: staging\nShared with: environment\n\nInside the container:\n  ssh -F /devbox/ssh/config staging\n")
				return nil
			})
			want := "\nConnected: staging\nShared with: environment\n\nInside the container:\n  ssh -F /devbox/ssh/config staging\n"
			// terminalOutput normalizes CRLF but leaves bare CR intact, catching double
			// conversion in cooked mode. Check raw bytes separately below for bare LF.
			if output != want {
				t.Fatalf("%q", output)
			}
		})
	}
	var out bytes.Buffer
	sshTerminalMessage(&out, "a\nb\n")
	if out.String() != "a\nb\n" {
		t.Fatal("converted redirected output")
	}
}

func TestSSHRawOutputReturnsToColumnZero(t *testing.T) {
	master, slave := testTerminal(t)
	state, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	state.Oflag &^= unix.OPOST
	if err := unix.IoctlSetTermios(int(slave.Fd()), unix.TCSETS, state); err != nil {
		t.Fatal(err)
	}
	sshTerminalMessage(slave, "\nConnected\nShared\n")
	expected := "\r\nConnected\r\nShared\r\n"
	if _, err := slave.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	actual, err := bufio.NewReader(master).ReadString(0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSuffix(actual, "\x00") != expected {
		t.Fatalf("raw terminal bytes: %q", actual)
	}
}

func TestSSHTerminalRestoredBeforeDisconnectOrError(t *testing.T) {
	for _, fails := range []bool{false, true} {
		_, file := testTerminal(t)
		original, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		failure := errors.New("cleanup failed")
		err = runSSHInTerminal(file, &out, func() error {
			raw := *original
			raw.Oflag &^= unix.OPOST
			raw.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG
			if err := unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, &raw); err != nil {
				return err
			}
			if fails {
				return failure
			}
			return nil
		})
		restored, readErr := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
		if readErr != nil || *restored != *original {
			t.Fatal("terminal not restored", restored, readErr)
		}
		if fails {
			if !errors.Is(err, failure) || strings.Contains(out.String(), "Disconnected.") {
				t.Fatal("suppressed failure", err, out.String())
			}
		} else if err != nil || out.String() != "\nDisconnected.\n" {
			t.Fatal(err, out.String())
		}
	}
}
