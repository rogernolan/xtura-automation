package watercalibration

import (
	"fmt"
	"math"
)

type Point struct {
	Percent float64 `yaml:"percent" json:"percent"`
	Litres  float64 `yaml:"litres" json:"litres"`
}

type Curve struct {
	points []Point
}

func New(points []Point) (Curve, error) {
	if len(points) < 2 {
		return Curve{}, fmt.Errorf("points: calibration curve must contain at least two points")
	}
	if points[0].Percent != 0 {
		return Curve{}, fmt.Errorf("percent: first calibration point must be 0%%")
	}
	if points[len(points)-1].Percent != 100 {
		return Curve{}, fmt.Errorf("percent: last calibration point must be 100%%")
	}

	copyOfPoints := append([]Point(nil), points...)
	for i, point := range copyOfPoints {
		if math.IsNaN(point.Percent) || math.IsInf(point.Percent, 0) {
			return Curve{}, fmt.Errorf("percent: point %d must be finite", i)
		}
		if math.IsNaN(point.Litres) || math.IsInf(point.Litres, 0) {
			return Curve{}, fmt.Errorf("litres: point %d must be finite", i)
		}
		if point.Litres < 0 {
			return Curve{}, fmt.Errorf("litres: point %d must be non-negative", i)
		}
		if i == 0 {
			continue
		}
		if point.Percent <= copyOfPoints[i-1].Percent {
			return Curve{}, fmt.Errorf("percent: point %d must be greater than point %d", i, i-1)
		}
		if point.Litres <= copyOfPoints[i-1].Litres {
			return Curve{}, fmt.Errorf("litres: point %d must be greater than point %d", i, i-1)
		}
	}

	return Curve{points: copyOfPoints}, nil
}

func (c Curve) LitresAtPercent(percent float64) float64 {
	if len(c.points) == 0 {
		return 0
	}
	if math.IsNaN(percent) {
		return math.NaN()
	}
	if percent <= c.points[0].Percent {
		return c.points[0].Litres
	}
	last := len(c.points) - 1
	if percent >= c.points[last].Percent {
		return c.points[last].Litres
	}
	for i := 1; i < len(c.points); i++ {
		upper := c.points[i]
		if percent <= upper.Percent {
			lower := c.points[i-1]
			fraction := (percent - lower.Percent) / (upper.Percent - lower.Percent)
			return lower.Litres + fraction*(upper.Litres-lower.Litres)
		}
	}
	return c.points[last].Litres
}

func (c Curve) CapacityLitres() float64 {
	if len(c.points) == 0 {
		return 0
	}
	return c.points[len(c.points)-1].Litres
}
