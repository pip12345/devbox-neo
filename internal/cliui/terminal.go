package cliui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// terminalScreen owns the terminal for one interactive command. Menus write their
// next frame into pending; only line input displays it. Long frames leave the
// alternate screen and print normally, preserving access to every choice.
type terminalScreen struct {
	terminal      *os.File
	pending       bytes.Buffer
	active        bool
	closed        bool
	showNextPlain bool
}

func (s *terminalScreen) Write(p []byte) (int, error) {
	if s.closed {
		return s.terminal.Write(p)
	}
	return s.pending.Write(p)
}

func (s *terminalScreen) choiceFrame() []byte {
	return bytes.Clone(s.pending.Bytes())
}

func (s *terminalScreen) retryChoice(frame []byte, hint string) {
	s.pending.Write(frame)
	s.pending.WriteString(hint)
}

func Terminal(out io.Writer) *os.File {
	if s, ok := out.(*terminalScreen); ok {
		return s.terminal
	}
	file, _ := out.(*os.File)
	return file
}

func (s *terminalScreen) leave() error {
	if !s.active {
		return nil
	}
	s.active = false
	_, err := io.WriteString(s.terminal, "\x1b[?1049l")
	return err
}

func (s *terminalScreen) finish() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.leave(); err != nil {
		return err
	}
	_, err := s.pending.WriteTo(s.terminal)
	return err
}

func (s *terminalScreen) show(prompt string) error {
	frame := s.pending.String() + prompt
	size, err := unix.IoctlGetWinsize(int(s.terminal.Fd()), unix.TIOCGWINSZ)
	fits := err == nil && size.Row > 2 && size.Col > 0 && FrameFits(frame, int(size.Row)-2, int(size.Col)) && !s.showNextPlain
	s.showNextPlain = false
	if !fits {
		if err := s.leave(); err != nil {
			return err
		}
	} else if !s.active {
		if _, err := io.WriteString(s.terminal, "\x1b[?1049h"); err != nil {
			return err
		}
		s.active = true
	}
	if s.active {
		if _, err := io.WriteString(s.terminal, "\x1b[H\x1b[2J"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(s.terminal, frame); err != nil {
		return err
	}
	s.pending.Reset()
	return nil
}

func (s *terminalScreen) afterInput() error {
	if !s.active {
		return nil
	}
	// The terminal may have wrapped an edited input line. Clear by absolute
	// position, not by guessing how far its cursor moved.
	_, err := io.WriteString(s.terminal, "\x1b[H\x1b[2J")
	return err
}

// Conservatively count non-ASCII glyphs as two cells. Combining, format,
// control, and non-SGR escape sequences use ordinary scrolling output rather
// than risking an underestimated frame size.
func FrameFits(frame string, maxRows, width int) bool {
	if !utf8.ValidString(frame) || strings.Count(frame, "\n")+1 > maxRows {
		return false
	}
	for _, line := range strings.Split(frame, "\n") {
		columns := 0
		for i := 0; i < len(line); {
			if line[i] == '\x1b' {
				end := i + 1
				if end >= len(line) || line[end] != '[' {
					return false
				}
				end++
				for end < len(line) && ((line[end] >= '0' && line[end] <= '9') || line[end] == ';') {
					end++
				}
				if end >= len(line) || line[end] != 'm' {
					return false
				}
				i = end + 1
				continue
			}
			r, n := utf8.DecodeRuneInString(line[i:])
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
				return false
			}
			if r > 0x7e {
				columns += 2
			} else {
				columns++
			}
			i += n
		}
		if columns >= width { // The last column can trigger an automatic wrap.
			return false
		}
	}
	return true
}
