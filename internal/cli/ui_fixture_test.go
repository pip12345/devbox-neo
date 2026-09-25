package cli

import (
	"context"
	"io"

	"devbox/internal/cliui"
	"github.com/spf13/cobra"
)

func testMenu(ctx context.Context, in io.Reader, out io.Writer) menu {
	return menu{Runner: cliui.New(ctx, in, out)}
}
func testMenuCommand(ctx context.Context, in io.Reader, out io.Writer, cmd *cobra.Command) menu {
	return menu{Runner: cliui.New(ctx, in, out), cmd: cmd}
}
