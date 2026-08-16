package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	listen := flag.String("listen", ":8090", "websocket listen address")
	capturePath := flag.String("capture", "", "path to an NDJSON capture to replay")
	loop := flag.Bool("loop", false, "replay the capture repeatedly")
	speed := flag.Float64("speed", 1.0, "replay pacing multiplier (higher = faster)")
	maxGap := flag.Duration("max-gap", 10*time.Second, "maximum inter-frame delay during replay")
	verbose := flag.Bool("verbose", false, "log inbound and outbound frames")
	flag.Parse()

	if *capturePath == "" {
		fmt.Fprintln(os.Stderr, "servsim: -capture is required")
		flag.Usage()
		os.Exit(2)
	}
	if *speed <= 0 {
		fmt.Fprintln(os.Stderr, "servsim: -speed must be positive")
		os.Exit(2)
	}
	items, err := parseCapture(*capturePath, *maxGap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "servsim: load capture: %v\n", err)
		os.Exit(1)
	}
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "servsim: capture contains no receive frames to replay")
		os.Exit(1)
	}

	logger := log.New(os.Stdout, "servsim ", log.LstdFlags)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &Server{
		addr:    *listen,
		capture: items,
		loop:    *loop,
		speed:   *speed,
		verbose: *verbose,
		logger:  logger,
	}
	logger.Printf("replaying %d receive frames from %s on %s (loop=%t)", len(items), *capturePath, *listen, *loop)
	if err := srv.serve(ctx); err != nil && err != http.ErrServerClosed {
		logger.Printf("servsim exited: %v", err)
		os.Exit(1)
	}
}
