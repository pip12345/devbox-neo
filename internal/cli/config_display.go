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
	label  string
	value  any
	origin string
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
	runes := []rune(prefix + text)
	width = max(1, width)
	for len(runes) > width {
		if _, err := fmt.Fprintln(out, string(runes[:width])); err != nil {
			return err
		}
		padding := []rune(continuation)
		if len(padding) >= width {
			padding = padding[:width-1]
		}
		runes = append(append([]rune(nil), padding...), runes[width:]...)
	}
	_, err := fmt.Fprintln(out, string(runes))
	return err
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

func printConfigRows(out io.Writer, rows []configDisplayRow, itemIndent string, width int) error {
	labelWidth, valueWidth, originWidth := 0, 0, 0
	for _, row := range rows {
		scalar, _ := configDisplayParts(row.value)
		labelWidth = max(labelWidth, utf8.RuneCountInString(row.label)+1)
		valueWidth = max(valueWidth, utf8.RuneCountInString(scalar))
		originWidth = max(originWidth, utf8.RuneCountInString(row.origin))
	}
	valueWidth = min(valueWidth, max(1, width-labelWidth-originWidth-4))
	for i, row := range rows {
		scalar, items := configDisplayParts(row.value)
		value := []rune(scalar)
		first := string(value[:min(len(value), valueWidth)])
		header := fmt.Sprintf("%-*s  %-*s", labelWidth, row.label+":", valueWidth, first)
		if row.origin != "" {
			header += "  " + row.origin
		}
		if err := writeConfigLine(out, "", strings.TrimRight(header, " "), itemIndent, width); err != nil {
			return err
		}
		if len(value) > valueWidth {
			if err := writeConfigLine(out, itemIndent, string(value[valueWidth:]), itemIndent, width); err != nil {
				return err
			}
		}
		for _, item := range items {
			if err := writeConfigLine(out, itemIndent+"- ", item, itemIndent+"  ", width); err != nil {
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

func (m menu) chooseConfig(rows []configDisplayRow) (int, error) {
	fmt.Fprintln(m.out, "\nSelect a setting")
	numbered := make([]configDisplayRow, len(rows))
	for i, row := range rows {
		numbered[i] = row
		numbered[i].label = fmt.Sprintf("  [%d] %s", i+1, row.label)
	}
	if err := printConfigRows(m.out, numbered, "       ", configDisplayWidth(m.out)); err != nil {
		return -1, err
	}
	return m.readChoice(len(rows), "Done")
}
