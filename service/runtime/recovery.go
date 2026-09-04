package runtime

import (
	"fmt"
	"log"
	"time"

	"github.com/getsentry/sentry-go"
)

// goSafe runs a runtime worker with a process-level panic report. Panics are
// local to goroutines, so the main goroutine cannot recover them itself.
func goSafe(logger *log.Logger, name string, fn func()) {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				sentry.WithScope(func(scope *sentry.Scope) {
					scope.SetTag("component", "empirebusd")
					scope.SetTag("worker", name)
					sentry.CurrentHub().Recover(recovered)
				})
				sentry.Flush(2 * time.Second)
				if logger != nil {
					logger.Printf("runtime worker %s panicked: %v", name, fmt.Sprint(recovered))
				}
			}
		}()
		fn()
	}()
}
