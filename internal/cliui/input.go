package cliui

import (
	"context"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Plain prompts show an initial value as context, not an implicit default:
// blank input still submits an empty replacement. Sensitive values are neither
// printed nor echoed by the terminal, and restoration uses the same reader.
func (r *Runner) plainText(request TextRequest) (value string, err error) {
	for _, notice := range r.notices {
		if _, err := fmt.Fprintln(r.Out, notice); err != nil {
			return "", err
		}
	}
	r.notices = nil
	if request.Sensitive && r.inputFile != nil {
		fd := int(r.inputFile.Fd())
		original, err := unix.IoctlGetTermios(fd, unix.TCGETS)
		if err != nil {
			return "", err
		}
		quiet := *original
		quiet.Lflag &^= unix.ECHO | unix.ECHONL
		if err := unix.IoctlSetTermios(fd, unix.TCSETS, &quiet); err != nil {
			return "", err
		}
		defer func() {
			restoreErr := unix.IoctlSetTermios(fd, unix.TCSETS, original)
			_, newlineErr := fmt.Fprintln(r.Out)
			err = errors.Join(err, restoreErr, newlineErr)
		}()
	}
	if request.Initial != "" && !request.Sensitive {
		if _, err := fmt.Fprintf(r.Out, "Current value: %s\nEnter a replacement; blank submits an empty value.\n", Safe(request.Initial)); err != nil {
			return "", err
		}
	}
	return r.Line(request.Prompt)
}

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
