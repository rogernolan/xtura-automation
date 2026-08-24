package waterhistory

import "time"

type Options struct {
	Directory      string
	Threshold      float64
	SettlingPeriod time.Duration
	GroupingWindow time.Duration
	Retention      time.Duration
}

type Sample struct {
	At           time.Time `json:"t"`
	FreshPercent *float64  `json:"fresh_percent,omitempty"`
	GreyPercent  *float64  `json:"grey_percent,omitempty"`
}

type Point struct {
	At           time.Time `json:"t"`
	FreshPercent *float64  `json:"fresh_percent,omitempty"`
	GreyPercent  *float64  `json:"grey_percent,omitempty"`
}

type Event struct {
	At   time.Time `json:"t"`
	Tank string    `json:"tank"`
	Kind string    `json:"kind"`
	From float64   `json:"from"`
	To   float64   `json:"to"`
	Used float64   `json:"used"`
}

type Marker struct {
	At     time.Time `json:"t"`
	Events []Event   `json:"events"`
}

type Summary struct {
	EventAt     *time.Time `json:"event_at,omitempty"`
	DaysSince   *float64   `json:"days_since,omitempty"`
	UsedPercent *float64   `json:"used_percent,omitempty"`
}

type Document struct {
	Samples      []Point  `json:"-"`
	ChartSamples []Point  `json:"chart_samples"`
	Events       []Event  `json:"events"`
	Markers      []Marker `json:"markers"`
	Fresh        Summary  `json:"fresh"`
	Grey         Summary  `json:"grey"`
}

const (
	TankFresh = "fresh"
	TankGrey  = "grey"
	KindFill  = "fill"
	KindEmpty = "empty"
)
