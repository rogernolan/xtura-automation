package runtime

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"empirebus-tests/service/host"
)

func TestAppSamplesAndPublishesPiStatus(t *testing.T) {
	rootCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := testRecordingConfig()
	cfg.Host.SampleInterval = 100 * time.Millisecond
	app, err := New(rootCtx, cfg, "", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}

	waitForCondition(t, "pi status sample", func() bool {
		return !app.HostStatus().SampledAt.IsZero()
	})

	stream, unsubscribe := app.Broker().Subscribe()
	t.Cleanup(unsubscribe)
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event := <-stream:
			if event.Type != "pi.state_changed" {
				continue
			}
			metrics, ok := event.Payload.(host.Metrics)
			if ok && !metrics.SampledAt.IsZero() {
				return
			}
		case <-timeout:
			t.Fatalf("timed out waiting for pi.state_changed event")
		}
	}
}
