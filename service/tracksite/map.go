// Package tracksite renders journey track files as RSS feed documents and
// map images for a small web site consumed by the InstaBlog agent.
package tracksite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
)

var (
	bgColor        = color.RGBA{0xF3, 0xF1, 0xEA, 0xFF}
	gridColor      = color.RGBA{0xE4, 0xE0, 0xD4, 0xFF}
	borderColor    = color.RGBA{0xC9, 0xC3, 0xB4, 0xFF}
	routeColor     = color.RGBA{0x1F, 0x5F, 0xD4, 0xFF}
	startColor     = color.RGBA{0x1E, 0x8E, 0x3E, 0xFF}
	endColor       = color.RGBA{0xD9, 0x30, 0x25, 0xFF}
	eventColor     = color.RGBA{0x5F, 0x63, 0x68, 0xFF}
	outlineColor   = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

// RenderTrack renders a track GeoJSON document as a size x size PNG map
// image. The track may be a single Feature with a LineString (or Point)
// geometry, or a FeatureCollection whose LineString feature is the route and
// whose Point features carrying a properties.event are engine waypoints.
func RenderTrack(data []byte, size int) ([]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("invalid map size %d", size)
	}
	route, events, err := parseTrack(data)
	if err != nil {
		return nil, err
	}
	if len(route) == 0 {
		return nil, fmt.Errorf("track contains no route geometry")
	}
	img := renderCanvas(route, events, size)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode map: %w", err)
	}
	return buf.Bytes(), nil
}

type point struct{ x, y float64 }

type trackGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

type trackProperties struct {
	Event string `json:"event"`
}

type trackFeature struct {
	Type       string          `json:"type"`
	Properties trackProperties `json:"properties"`
	Geometry   trackGeometry   `json:"geometry"`
}

type trackTop struct {
	Type     string            `json:"type"`
	Features []json.RawMessage `json:"features"`
}

func parseTrack(data []byte) (route [][]float64, events [][]float64, err error) {
	var top trackTop
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, nil, fmt.Errorf("parse track: %w", err)
	}
	switch top.Type {
	case "Feature":
		return featurePoints(data)
	case "FeatureCollection":
		var fallback [][]float64
		for _, raw := range top.Features {
			var f trackFeature
			if err := json.Unmarshal(raw, &f); err != nil {
				continue
			}
			switch f.Geometry.Type {
			case "LineString":
				route = append(route, lineCoordinates(f.Geometry.Coordinates)...)
			case "Point":
				pt := pointCoordinates(f.Geometry.Coordinates)
				if pt == nil {
					continue
				}
				if f.Properties.Event != "" {
					events = append(events, pt)
				} else if len(route) == 0 && len(fallback) == 0 {
					fallback = append(fallback, pt)
				}
			}
		}
		if len(route) == 0 {
			route = fallback
		}
		return route, events, nil
	default:
		return nil, nil, fmt.Errorf("unsupported track type %q", top.Type)
	}
}

func featurePoints(data []byte) ([][]float64, [][]float64, error) {
	var f trackFeature
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, nil, fmt.Errorf("parse feature: %w", err)
	}
	switch f.Geometry.Type {
	case "LineString":
		return lineCoordinates(f.Geometry.Coordinates), nil, nil
	case "Point", "MultiPoint":
		pt := pointCoordinates(f.Geometry.Coordinates)
		if pt == nil {
			return nil, nil, fmt.Errorf("invalid point geometry")
		}
		return [][]float64{pt}, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported geometry type %q", f.Geometry.Type)
	}
}

func lineCoordinates(raw json.RawMessage) [][]float64 {
	var pts [][]float64
	_ = json.Unmarshal(raw, &pts)
	out := make([][]float64, 0, len(pts))
	for _, p := range pts {
		if len(p) >= 2 {
			out = append(out, []float64{p[0], p[1]})
		}
	}
	return out
}

func pointCoordinates(raw json.RawMessage) []float64 {
	var p []float64
	_ = json.Unmarshal(raw, &p)
	if len(p) >= 2 {
		return []float64{p[0], p[1]}
	}
	return nil
}

// renderCanvas draws the route and markers onto a size x size RGBA canvas.
func renderCanvas(route, events [][]float64, size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), image.NewUniform(bgColor), image.Point{}, draw.Src)
	drawGrid(img, size)
	routePts, eventPts := project(route, events, size)
	lineWidth := clamp(size/160, 3, 24)
	if len(routePts) > 0 {
		for i := 0; i+1 < len(routePts); i++ {
			strokeLine(img, routePts[i], routePts[i+1], lineWidth+2, outlineColor)
		}
		for i := 0; i+1 < len(routePts); i++ {
			strokeLine(img, routePts[i], routePts[i+1], lineWidth, routeColor)
		}
	}
	markerRadius := clamp(size/60, 6, 40)
	for _, ep := range eventPts {
		stampDisc(img, ep, markerRadius/2+2, outlineColor)
		stampDisc(img, ep, markerRadius/2, eventColor)
	}
	if len(routePts) > 0 {
		start := routePts[0]
		stampDisc(img, start, markerRadius+2, outlineColor)
		stampDisc(img, start, markerRadius, startColor)
		end := routePts[len(routePts)-1]
		stampDisc(img, end, markerRadius+2, outlineColor)
		stampDisc(img, end, markerRadius, endColor)
	}
	strokeBorder(img, size, borderColor)
	return img
}

func drawGrid(img *image.RGBA, size int) {
	for i := 1; i < 8; i++ {
		x := i * size / 8
		for y := 0; y < size; y++ {
			img.Set(x, y, gridColor)
		}
		yy := i * size / 8
		for x := 0; x < size; x++ {
			img.Set(x, yy, gridColor)
		}
	}
}

// project maps lon/lat coordinates to canvas pixels with a Web Mercator fit
// to the combined bounds, padded so the drawing fills ~80% of the canvas.
// Degenerate bounds (single point) fall back to a one arc-minute span.
func project(route, events [][]float64, size int) ([]point, []point) {
	all := make([][]float64, 0, len(route)+len(events))
	all = append(all, route...)
	all = append(all, events...)
	if len(all) == 0 {
		return nil, nil
	}
	minX, maxX, minY, maxY := mercatorBounds(all)
	const minSpan = 1.0 / 60.0 // one arc-minute fallback in projected units
	spanX := math.Max(maxX-minX, minSpan)
	spanY := math.Max(maxY-minY, minSpan)
	scale := float64(size) * 0.8 / math.Max(spanX, spanY)
	midX, midY := (minX+maxX)/2, (minY+maxY)/2
	offset := float64(size) / 2
	toPoint := func(p []float64) point {
		wx, wy := mercator(p[0], p[1])
		return point{x: offset + (wx - midX) * scale, y: offset - (wy - midY) * scale}
	}
	routeOut := make([]point, 0, len(route))
	for _, p := range route {
		routeOut = append(routeOut, toPoint(p))
	}
	eventsOut := make([]point, 0, len(events))
	for _, p := range events {
		eventsOut = append(eventsOut, toPoint(p))
	}
	return routeOut, eventsOut
}

func mercatorBounds(points [][]float64) (minX, maxX, minY, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, p := range points {
		x, y := mercator(p[0], p[1])
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	return
}

func mercator(lon, lat float64) (x, y float64) {
	lonRad := lon * math.Pi / 180
	latRad := lat * math.Pi / 180
	return lonRad, math.Log(math.Tan(math.Pi/4 + latRad/2))
}

func strokeLine(img *image.RGBA, a, b point, width int, c color.RGBA) {
	if width <= 0 {
		return
	}
	dx, dy := b.x-a.x, b.y-a.y
	dist := math.Hypot(dx, dy)
	if dist == 0 {
		stampDisc(img, a, width/2, c)
		return
	}
	steps := int(math.Ceil(dist * 2))
	radius := float64(width) / 2
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		stampDisc(img, point{x: a.x + dx*t, y: a.y + dy*t}, int(radius), c)
	}
}

func stampDisc(img *image.RGBA, center point, radius int, c color.RGBA) {
	if radius < 0 {
		return
	}
	cx, cy := int(math.Round(center.x)), int(math.Round(center.y))
	b := img.Bounds()
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if dx*dx+dy*dy > radius*radius {
				continue
			}
			x, y := cx+dx, cy+dy
			if x < b.Min.X || x > b.Max.X-1 || y < b.Min.Y || y > b.Max.Y-1 {
				continue
			}
			img.Set(x, y, c)
		}
	}
}

func strokeBorder(img *image.RGBA, size int, c color.RGBA) {
	if size < 4 {
		return
	}
	for i := 0; i < 2; i++ {
		lo := i
		hi := size - 1 - i
		for px := lo; px <= hi; px++ {
			img.Set(px, lo, c)
			img.Set(px, hi, c)
		}
		for py := lo; py <= hi; py++ {
			img.Set(lo, py, c)
			img.Set(hi, py, c)
		}
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}