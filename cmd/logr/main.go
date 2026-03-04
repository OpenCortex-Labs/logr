package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/OpenCortex-Labs/logr/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM,
	)
	defer stop()

	if err := cli.NewRootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
