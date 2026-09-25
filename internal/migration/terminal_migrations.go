package migration

import (
	"devbox/internal/cliui"
	"io"
)

type reportPaint struct{ enabled bool }

// Detect the output terminal, not stdin: redirected output and saved reports
// must stay plain even when the user is answering prompts in a terminal.
func reportColors(out io.Writer) reportPaint {
	return reportPaint{enabled: cliui.Colors(out).Enabled()}
}

func (p reportPaint) style(code, text string) string {
	if !p.enabled {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (p reportPaint) heading(text string) string { return p.style("1", text) }

func (p reportPaint) item(i Item) string {
	key := display(i.Key)
	if i.Kind == "session" || i.Kind == "profile" {
		return p.heading(key)
	}
	return key
}

// InventoryItemLabel shares preview status and styling with the selection menu;
// metadata errors remain visible even when an item is explicitly skipped.
func InventoryItemLabel(out io.Writer, i Item) string {
	return reportColors(out).itemLabel(i, reportItemState(i, nil))
}

func (p reportPaint) itemLabel(i Item, state string) string {
	label := p.status(state) + " " + p.item(i)
	if i.Harness != "" && i.Kind != "auth" {
		label += " (" + display(i.Harness) + ")"
	}
	return label
}

func (p reportPaint) status(state string) string {
	code := "36"
	switch state {
	case "Error", "Failed":
		code = "31"
	case "Warning":
		code = "33"
	case "Skipped":
		code = "2"
	case "Imported", "Staged":
		code = "32"
	}
	return p.style(code, "["+state+"]")
}
