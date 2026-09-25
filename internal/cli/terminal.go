package cli

import (
	"io"
	"os"

	"devbox/internal/cliui"
)

// Table renderers use the same terminal capabilities and palette as screens.
type terminalPaint struct{ paint cliui.Paint }

func terminalColors(out io.Writer) terminalPaint  { return terminalPaint{cliui.Colors(out)} }
func (p terminalPaint) dim(text string) string    { return p.paint.Dim(text) }
func (p terminalPaint) strong(text string) string { return p.paint.Strong(text) }
func (p terminalPaint) green(text string) string  { return p.paint.Green(text) }
func terminal(file *os.File) bool                 { return cliui.IsTerminal(file) }
