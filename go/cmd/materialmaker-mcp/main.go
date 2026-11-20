package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)
import "github.com/davidherring123/materialmaker-mcp/go/internal/hostcmd"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := hostcmd.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
