package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"empirebus-tests/service/api/httpapi"
	"empirebus-tests/service/config"
	"empirebus-tests/service/runtime"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "path to the service config")
	flag.Parse()

	cfg, err := config.LoadFile(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	normalized, err := cfg.Normalize()
	if err != nil {
		log.Fatalf("normalize config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := log.New(os.Stdout, "", log.LstdFlags)
	app, err := runtime.New(ctx, *cfg, configPath, logger)
	if err != nil {
		log.Fatalf("start app: %v", err)
	}
	logger.Printf("empirebusd starting: config=%s listen=%s", configPath, normalized.API.Listen)
	server := &http.Server{
		Addr:              normalized.API.Listen,
		Handler:           httpapi.New(app).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		logger.Printf("empirebusd shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	listener, err := net.Listen("tcp", normalized.API.Listen)
	if err != nil {
		log.Fatalf("listen %s: %v", normalized.API.Listen, err)
	}
	logger.Printf("empirebusd listening on %s", normalized.API.Listen)
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
