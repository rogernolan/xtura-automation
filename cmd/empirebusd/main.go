package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"empirebus-tests/service/api/httpapi"
	"empirebus-tests/service/buildinfo"
	"empirebus-tests/service/config"
	"empirebus-tests/service/runtime"
	"github.com/getsentry/sentry-go"
)

const defaultSentryDSN = "https://7974958093e997a45c12344488e04cc0@o4511755059462144.ingest.de.sentry.io/4512026879262800"

type sentryBreadcrumbWriter struct{}

func (sentryBreadcrumbWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		sentry.AddBreadcrumb(&sentry.Breadcrumb{
			Category: "log",
			Message:  string(p),
			Level:    sentry.LevelInfo,
		})
	}
	return len(p), nil
}

func sentryDSN() string {
	if dsn := os.Getenv("SENTRY_DSN"); dsn != "" {
		return dsn
	}
	return defaultSentryDSN
}

func captureError(message string, err error) {
	if err == nil {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("component", "empirebusd")
		scope.SetTag("operation", message)
		sentry.CaptureException(err)
	})
}

func fatalError(message string, err error) {
	captureError(message, err)
	sentry.Flush(2 * time.Second)
	log.Fatalf("%s: %v", message, err)
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "path to the service config")
	flag.Parse()

	environment := os.Getenv("XTURA_ENVIRONMENT")
	if environment == "" {
		environment = "production"
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:         sentryDSN(),
		Release:     buildinfo.Current().GitSHA,
		Environment: environment,
	}); err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}
	defer sentry.Flush(2 * time.Second)
	defer func() {
		if recovered := recover(); recovered != nil {
			sentry.CurrentHub().Recover(recovered)
			sentry.Flush(2 * time.Second)
			panic(recovered)
		}
	}()

	cfg, err := config.LoadFile(configPath)
	if err != nil {
		fatalError("load config", err)
	}
	if generated, err := config.EnsureVAPIDKeys(configPath, cfg); err != nil {
		fatalError("configure VAPID keys", err)
	} else if generated {
		log.Printf("generated and persisted VAPID keys in %s", configPath)
	}
	normalized, err := cfg.Normalize()
	if err != nil {
		fatalError("normalize config", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := log.New(io.MultiWriter(os.Stdout, sentryBreadcrumbWriter{}), "", log.LstdFlags)
	app, err := runtime.New(ctx, *cfg, configPath, logger)
	if err != nil {
		fatalError("start app", err)
	}
	version := buildinfo.Current()
	sentry.NewLogger(ctx).Info().Emit(fmt.Sprintf(
		"empirebusd started: version=%s environment=%s config=%s listen=%s",
		version.GitSHA,
		environment,
		configPath,
		normalized.API.Listen,
	))
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
		fatalError(fmt.Sprintf("listen %s", normalized.API.Listen), err)
	}
	logger.Printf("empirebusd listening on %s", normalized.API.Listen)
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fatalError("server", err)
	}
}
