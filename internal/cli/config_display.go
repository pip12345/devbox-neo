package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// Display rows never serve as editable source. Menus supply redacted source or
// effective values; --show supplies the resolver's already-redacted view.
type configDisplayRow struct {
	label        string
	value        any
	origin       string
	entryOrigins []string
	command      bool
}

func configSourceLabel(sources []string) string {
	if len(sources) == 0 {
		return "default"
	}
	labels := make([]string, len(sources))
	for i, source := range sources {
		if source == "built-in default" {
			source = "default"
		}
		labels[i] = source
	}
	return strings.Join(labels, " + ")
}

func configSourceForScope(scope, source string) string {
	source = configSourceLabel([]string{source})
	if source == "global" || source == "profile" || source == "project" {
		if source != scope {
			return "inherited - " + source
		}
	}
	return source
}

func configEntryOrigins(scope string, sources []string) []string {
	origins := make([]string, len(sources))
	for i, source := range sources {
		origins[i] = configSourceForScope(scope, source)
	}
	return origins
}

func configDisplayWidth(out io.Writer) int {
	const readableWidth = 80
	if file, ok := out.(*os.File); ok {
		if size, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ); err == nil && size.Col > 0 {
			return min(readableWidth, int(size.Col))
		}
	}
	return readableWidth
}

// Hard wrapping preserves every character, including whitespace in argv and
// paths. Continuations have no bullet/number so they cannot look like new items.
func writeConfigLine(out io.Writer, prefix, text, continuation string, width int) error {
	return writeStyledConfigLine(out, prefix, text, continuation, width, nil)
}

func writeStyledConfigLine(out io.Writer, prefix, text, continuation string, width int, style func(string) string) error {
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

func configScalar(value any, listItem bool) string {
	switch value := value.(type) {
	case nil:
		return "None"
	case string:
		if value == "" {
			if listItem {
				return "(empty)"
			}
			return "None"
		}
		return displayCell(value)
	case bool:
		if value {
			return "Yes"
		}
		return "No"
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64)
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return "Invalid value"
		}
		return string(b)
	}
}

func configDisplayParts(value any) (scalar string, items []string) {
	switch value := value.(type) {
	case []string:
		for _, item := range value {
			items = append(items, configScalar(item, true))
		}
	case []any:
		for _, item := range value {
			items = append(items, configScalar(item, true))
		}
	default:
		return configScalar(value, false), nil
	}
	if len(items) == 0 {
		return "None", nil
	}
	return "", items
}

func (r configDisplayRow) parts() (scalar string, items []string) {
	if !r.command {
		return configDisplayParts(r.value)
	}
	var args []string
	switch value := r.value.(type) {
	case []string:
		args = value
	case []any:
		for _, arg := range value {
			text, ok := arg.(string)
			if !ok {
				return "Invalid value", nil
			}
			args = append(args, text)
		}
	default:
		return configScalar(r.value, false), nil
	}
	if len(args) == 0 {
		return "None", nil
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return displayCell(strings.Join(quoted, " ")), nil
}

func printConfigRows(out io.Writer, rows []configDisplayRow, itemIndent string, width int) error {
	return renderConfigRows(out, rows, itemIndent, width, false)
}

// The menu uses a labelled table; --show retains named fields without menu
// numbers. Both use the same value wrapping and per-entry source placement.
func renderConfigRows(out io.Writer, rows []configDisplayRow, itemIndent string, width int, menuTable bool) error {
	paint := terminalColors(out)
	labelWidth, valueWidth, originWidth := 0, 0, 0
	for _, row := range rows {
		scalar, items := row.parts()
		label := row.label
		if !menuTable {
			label += ":"
		}
		labelWidth = max(labelWidth, utf8.RuneCountInString(label))
		valueWidth = max(valueWidth, utf8.RuneCountInString(scalar))
		if len(items) == 0 {
			originWidth = max(originWidth, utf8.RuneCountInString(row.origin))
		}
		if !row.command && (len(row.entryOrigins) > 0 || len(items) > 0 && row.origin != "") && len(row.entryOrigins) != len(items) {
			return fmt.Errorf("entry source count differs from values for %s", row.label)
		}
		for _, origin := range row.entryOrigins {
			originWidth = max(originWidth, utf8.RuneCountInString(origin))
		}
	}
	if menuTable {
		labelWidth = max(labelWidth, len("        Setting"))
		valueWidth = max(valueWidth, len("Value"))
		originWidth = max(originWidth, len("Source"))
	}
	valueWidth = min(valueWidth, max(1, width-labelWidth-originWidth-4))
	originColumn := labelWidth + valueWidth + 4
	if menuTable {
		header := fmt.Sprintf("%-*s  %-*s  Source", labelWidth, "        Setting", valueWidth, "Value")
		if utf8.RuneCountInString(header) <= width {
			if _, err := fmt.Fprintln(out, paint.dim(header)); err != nil {
				return err
			}
		} else if err := writeConfigLine(out, "", header, itemIndent, width); err != nil {
			return err
		}
	}
	for i, row := range rows {
		scalar, items := row.parts()
		label := row.label
		if !menuTable {
			label += ":"
		}
		prefix := fmt.Sprintf("%-*s  ", labelWidth, label)
		origin := row.origin
		if len(items) > 0 {
			origin = ""
		}
		if err := writeConfigValue(out, prefix, scalar, origin, itemIndent, originColumn, width, paint); err != nil {
			return err
		}
		bullet := "- "
		if menuTable {
			bullet = "• "
		}
		for j, item := range items {
			origin := ""
			if len(row.entryOrigins) > 0 {
				origin = row.entryOrigins[j]
			}
			if err := writeConfigValue(out, itemIndent+bullet, item, origin, itemIndent+"  ", originColumn, width, paint); err != nil {
				return err
			}
		}
		if len(items) > 0 && i+1 < len(rows) {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeConfigValue(out io.Writer, prefix, text, origin, continuation string, originColumn, width int, paint terminalPaint) error {
	if origin == "" {
		line := prefix + text
		if text == "" {
			line = strings.TrimRight(prefix, " ")
		}
		return writeConfigLine(out, "", line, continuation, width)
	}
	budget := max(1, originColumn-utf8.RuneCountInString(prefix)-2)
	value := []rune(text)
	first := string(value[:min(len(value), budget)])
	padding := strings.Repeat(" ", max(2, originColumn-utf8.RuneCountInString(prefix+first)))
	line := prefix + first + padding + origin
	if utf8.RuneCountInString(line) > width {
		if err := writeConfigLine(out, "", line, continuation, width); err != nil {
			return err
		}
	} else {
		visible := first
		if text == "None" {
			visible = paint.dim(first)
		} else if origin == "profile" || origin == "project" || origin == "global" {
			visible = paint.strong(first)
		}
		if _, err := fmt.Fprintln(out, prefix+visible+padding+paint.dim(origin)); err != nil {
			return err
		}
	}
	if len(value) > budget {
		var style func(string) string
		if origin == "profile" || origin == "project" || origin == "global" {
			style = paint.strong
		}
		return writeStyledConfigLine(out, continuation, string(value[budget:]), continuation, width, style)
	}
	return nil
}

func (m menu) chooseConfig(rows []configDisplayRow) (int, error) {
	fmt.Fprintln(m.out)
	numbered := make([]configDisplayRow, len(rows))
	for i, row := range rows {
		numbered[i] = row
		numbered[i].label = menuPrefix(i+1) + row.label
	}
	if err := renderConfigRows(m.out, numbered, "        ", configDisplayWidth(m.out), true); err != nil {
		return -1, err
	}
	return m.readChoice(len(rows), "Done")
}
