package watercalibration

import (
	"math"
	"strings"
	"testing"
)

func TestNewAndLitresAtPercent(t *testing.T) {
	tests := []struct {
		name    string
		points  []Point
		percent float64
		want    float64
	}{
		{name: "fresh curve", points: []Point{{Percent: 0, Litres: 0}, {Percent: 50, Litres: 60.2}, {Percent: 100, Litres: 138.9}}, percent: 75, want: 99.55},
		{name: "exact interior point", points: []Point{{Percent: 0, Litres: 0}, {Percent: 50, Litres: 60.2}, {Percent: 100, Litres: 138.9}}, percent: 50, want: 60.2},
		{name: "lower clamp", points: []Point{{Percent: 0, Litres: 0}, {Percent: 100, Litres: 138.9}}, percent: -5, want: 0},
		{name: "upper clamp", points: []Point{{Percent: 0, Litres: 0}, {Percent: 100, Litres: 138.9}}, percent: 105, want: 138.9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			curve, err := New(tt.points)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if got := curve.LitresAtPercent(tt.percent); math.Abs(got-tt.want) > 0.0001 {
				t.Errorf("LitresAtPercent(%v) = %v L, want %v L", tt.percent, got, tt.want)
			}
		})
	}
}

func TestLitresAtPercentCentralInterpolation(t *testing.T) {
	curve, err := New([]Point{{Percent: 0, Litres: 0}, {Percent: 50, Litres: 60.2}, {Percent: 100, Litres: 138.9}})
	if err != nil {
		t.Fatal(err)
	}
	if got := curve.LitresAtPercent(75); math.Abs(got-99.55) > 0.0001 {
		t.Fatalf("75%% = %v L, want 99.55 L", got)
	}
}

func TestCapacityLitres(t *testing.T) {
	curve, err := New([]Point{{Percent: 0, Litres: 0}, {Percent: 40, Litres: 48.1}, {Percent: 100, Litres: 138.9}})
	if err != nil {
		t.Fatal(err)
	}
	if got := curve.CapacityLitres(); got != 138.9 {
		t.Fatalf("CapacityLitres() = %v, want 138.9", got)
	}
}

func TestNewRejectsInvalidCurves(t *testing.T) {
	tests := []struct {
		name   string
		points []Point
		field  string
	}{
		{name: "absent curve", field: "points"},
		{name: "too few points", points: []Point{{Percent: 0, Litres: 0}}, field: "points"},
		{name: "first percentage missing zero", points: []Point{{Percent: 1, Litres: 0}, {Percent: 100, Litres: 10}}, field: "percent"},
		{name: "last percentage missing one hundred", points: []Point{{Percent: 0, Litres: 0}, {Percent: 99, Litres: 10}}, field: "percent"},
		{name: "duplicate percentages", points: []Point{{Percent: 0, Litres: 0}, {Percent: 50, Litres: 5}, {Percent: 50, Litres: 7}, {Percent: 100, Litres: 10}}, field: "percent"},
		{name: "descending percentages", points: []Point{{Percent: 0, Litres: 0}, {Percent: 60, Litres: 5}, {Percent: 50, Litres: 7}, {Percent: 100, Litres: 10}}, field: "percent"},
		{name: "non-increasing litres", points: []Point{{Percent: 0, Litres: 0}, {Percent: 50, Litres: 5}, {Percent: 100, Litres: 5}}, field: "litres"},
		{name: "non-finite percentage", points: []Point{{Percent: 0, Litres: 0}, {Percent: math.NaN(), Litres: 5}, {Percent: 100, Litres: 10}}, field: "percent"},
		{name: "infinite percentage", points: []Point{{Percent: 0, Litres: 0}, {Percent: math.Inf(1), Litres: 5}, {Percent: 100, Litres: 10}}, field: "percent"},
		{name: "non-finite litres", points: []Point{{Percent: 0, Litres: 0}, {Percent: 50, Litres: math.NaN()}, {Percent: 100, Litres: 10}}, field: "litres"},
		{name: "infinite litres", points: []Point{{Percent: 0, Litres: 0}, {Percent: 50, Litres: math.Inf(1)}, {Percent: 100, Litres: 10}}, field: "litres"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.points)
			if err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.field) {
				t.Errorf("New() error = %q, want field-specific %q error", err, tt.field)
			}
		})
	}
}
