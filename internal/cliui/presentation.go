package cliui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func IsTerminal(file *os.File) bool {
	_, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
	return err == nil
}

type Paint struct{ enabled bool }

func (p Paint) Enabled() bool { return p.enabled }

func Colors(out io.Writer) Paint {
	_, noColor := os.LookupEnv("NO_COLOR")
	file := Terminal(out)
	return Paint{enabled: file != nil && !noColor && os.Getenv("TERM") != "dumb" && IsTerminal(file)}
}
func (p Paint) Dim(s string) string {
	if !p.enabled || s == "" {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
}
func (p Paint) Strong(s string) string {
	if !p.enabled || s == "" {
		return s
	}
	return "\x1b[1m" + s + "\x1b[0m"
}
func (p Paint) Green(s string) string {
	if !p.enabled || s == "" {
		return s
	}
	return "\x1b[32m" + s + "\x1b[0m"
}
func Width(out io.Writer) int {
	if file := Terminal(out); file != nil {
		if size, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ); err == nil && size.Col > 0 {
			return min(80, int(size.Col))
		}
	}
	return 80
}
func Title(out io.Writer, title string) error {
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	return WriteLine(out, "", title, "", Width(out), Colors(out).Strong)
}
func Hint(out io.Writer, text string) error {
	return WriteLine(out, "", text, "", Width(out), Colors(out).Dim)
}

// Wrapping preserves spaces in paths and argv; continuation lines cannot
// masquerade as another numbered choice.
func WriteLine(out io.Writer, prefix, text, continuation string, width int, style func(string) string) error {
	printLine := func(line string) error {
		if style != nil {
			line = style(line)
		}
		_, err := fmt.Fprintln(out, line)
		return err
	}
	runes := []rune(prefix + text)
	width = max(1, width)
	for len(runes) > width {
		if err := printLine(string(runes[:width])); err != nil {
			return err
		}
		padding := []rune(continuation)
		if len(padding) >= width {
			padding = padding[:width-1]
		}
		runes = append(append([]rune(nil), padding...), runes[width:]...)
	}
	return printLine(string(runes))
}
func Safe(text string) string {
	if strings.IndexFunc(text, func(r rune) bool { return r < 32 || r >= 127 && r < 160 }) >= 0 {
		return strconv.QuoteToASCII(text)
	}
	return text
}
