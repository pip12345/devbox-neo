package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Menus stay in canonical terminal mode: the terminal provides line editing,
// and promptReader handles cancellation without an abandoned stdin goroutine.
// Values remain separate from labels, so redacted display text is never saved.
type menu struct {
	ctx context.Context
	in  *bufio.Reader
	out io.Writer
}

func (m menu) line(prompt string) (string, error) {
	if err := m.ctx.Err(); err != nil {
		return "", err
	}
	if _, err := fmt.Fprint(m.out, prompt); err != nil {
		return "", err
	}
	// Enter submits an operation. EOF must not submit a partially typed value.
	line, err := m.in.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func writeMenuTitle(out io.Writer, title string) error {
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	return writeStyledConfigLine(out, "", title, "", configDisplayWidth(out), terminalColors(out).strong)
}

func writeMenuHint(out io.Writer, text string) error {
	return writeStyledConfigLine(out, "", text, "", configDisplayWidth(out), terminalColors(out).dim)
}

func writeMenuChoices(out io.Writer, title string, choices []string) error {
	if err := writeMenuTitle(out, title); err != nil {
		return err
	}
	for i, choice := range choices {
		prefix := menuPrefix(i + 1)
		if err := writeConfigLine(out, prefix, choice, strings.Repeat(" ", len(prefix)), configDisplayWidth(out)); err != nil {
			return err
		}
	}
	return nil
}

const menuChoicePrompt = "\n   Choose a number > "

func (m menu) choose(title string, choices []string, back string) (int, error) {
	if err := writeMenuChoices(m.out, title, choices); err != nil {
		return -1, err
	}
	return m.readChoice(len(choices), back)
}

func menuPrefix(number int) string {
	return fmt.Sprintf("   %-4s ", fmt.Sprintf("[%d]", number))
}

func (m menu) readChoice(count int, back string) (int, error) {
	fmt.Fprintf(m.out, "\n%s%s\n", menuPrefix(0), back)
	for {
		line, err := m.line(menuChoicePrompt)
		if err != nil {
			return -1, err
		}
		line = strings.TrimSpace(line)
		if line == "0" || line == "q" {
			return -1, nil
		}
		n, err := strconv.Atoi(line)
		if err == nil && n >= 1 && n <= count {
			return n - 1, nil
		}
		fmt.Fprintf(m.out, "Choose 1–%d, or 0 to %s.\n", count, strings.ToLower(back))
	}
}
