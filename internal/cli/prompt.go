package cli

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

// Wait for terminal input without an abandoned stdin-reading goroutine when
// SIGINT cancels the command. Buffered lines still go through the same reader.
type terminalReader struct {
	ctx  context.Context
	file *os.File
}

func (r terminalReader) Read(p []byte) (int, error) {
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		descriptors := []unix.PollFd{{Fd: int32(r.file.Fd()), Events: unix.POLLIN}}
		n, err := unix.Poll(descriptors, 100)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n > 0 {
			return r.file.Read(p)
		}
	}
}
func promptReader(cmd *cobra.Command) *bufio.Reader {
	var input io.Reader = cmd.InOrStdin()
	if f, ok := input.(*os.File); ok && terminal(f) {
		input = terminalReader{ctx: cmd.Context(), file: f}
	}
	return bufio.NewReader(input)
}
