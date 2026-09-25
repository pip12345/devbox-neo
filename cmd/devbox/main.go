package main

import (
	"context"
	"os"

	"devbox/internal/cli"
)

func main() {
	ctx, cancel := cli.SignalContext(context.Background())
	defer cancel()
	if code := cli.Execute(ctx, cli.New()); code != 0 {
		os.Exit(code)
	}
}
