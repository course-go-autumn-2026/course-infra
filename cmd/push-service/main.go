// Command push-service runs the educational TripGo Push Service stub.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/course-go-autumn-2026/tripgo-infra/internal/buildinfo"
	"github.com/course-go-autumn-2026/tripgo-infra/internal/pushservice"
)

func main() {
	showVersion := flag.Bool("version", false, "print build metadata")
	flag.Parse()

	info := buildinfo.Current("tripgo-push-service")
	if *showVersion {
		_, _ = fmt.Fprintln(os.Stdout, info.String())
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := (pushservice.App{}).Run(ctx)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Push Service failed: %v\n", err)
		os.Exit(1)
	}
}
