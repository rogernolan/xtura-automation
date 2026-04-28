package heating

import "testing"

func TestValidateTargetCelsiusAcceptsMinimumTarget(t *testing.T) {
	t.Parallel()
	if err := ValidateTargetCelsius(5.0); err != nil {
		t.Fatalf("expected 5.0C to be accepted, got %v", err)
	}
}
