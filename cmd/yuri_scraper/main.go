package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// Version and ProjectRoot are set by the release or user-install build.
var Version = "dev"
var ProjectRoot string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
