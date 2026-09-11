package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"devbox/internal/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if code := cli.Execute(ctx, cli.New()); code != 0 {
		os.Exit(code)
	}
}
