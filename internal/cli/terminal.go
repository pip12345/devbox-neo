package cli

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

type terminalPaint struct{ enabled bool }

func terminalColors(out io.Writer) terminalPaint {
	_, noColor := os.LookupEnv("NO_COLOR")
	file, ok := out.(*os.File)
	return terminalPaint{enabled: ok && !noColor && os.Getenv("TERM") != "dumb" && terminal(file)}
}

func (p terminalPaint) dim(text string) string {
	if !p.enabled || text == "" {
		return text
	}
	return "\x1b[2m" + text + "\x1b[0m"
}

func (p terminalPaint) strong(text string) string {
	if !p.enabled || text == "" {
		return text
	}
	return "\x1b[1m" + text + "\x1b[0m"
}

func (p terminalPaint) green(text string) string {
	if !p.enabled || text == "" {
		return text
	}
	return "\x1b[32m" + text + "\x1b[0m"
}

func terminal(file *os.File) bool {
	var state syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&state)))
	return errno == 0
}
