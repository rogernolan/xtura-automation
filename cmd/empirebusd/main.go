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
	if generated, err := config.EnsureVAPIDKeys(configPath, cfg); err != nil {
		log.Fatalf("configure VAPID keys: %v", err)
	} else if generated {
		log.Printf("generated and persisted VAPID keys in %s", configPath)
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
	logger.Printf(
		"empirebusd garmin target: ws_url=%s origin=%s heartbeat=%s trace_window=%s",
		normalized.Garmin.WSURL,
		normalized.Garmin.Origin,
		normalized.Garmin.HeartbeatInterval,
		normalized.Garmin.TraceWindow,
	)
	if normalized.Location.Enabled {
		logger.Printf(
			"empirebusd location provider: provider=%s endpoint=%s poll_interval=%s timezone_provider=%s timezone_update=%t",
			normalized.Location.Provider,
			normalized.Location.RUTX50.Endpoint,
			normalized.Location.PollInterval,
			normalized.Location.Timezone.Provider,
			normalized.Location.TimezoneUpdate.Enabled,
		)
	}
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
