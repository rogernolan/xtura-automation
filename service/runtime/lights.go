package runtime

import (
	"time"

	domainlights "empirebus-tests/service/domains/lights"
)

func recordExteriorSignal(state domainlights.State, on bool, at time.Time) domainlights.State {
	state.ExternalKnown = true
	state.ExternalOn = on
	state.LastUpdatedAt = &at
	return state
}
