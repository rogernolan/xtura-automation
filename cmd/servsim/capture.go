package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"empirebus-tests/heating"
)

type captureRecord struct {
	At        time.Time          `json:"at"`
	Direction string             `json:"direction"`
	Message   string             `json:"message,omitempty"`
	Frame     *heating.WireFrame `json:"frame,omitempty"`
}

type replayItem struct {
	delay   time.Duration
	message string
}

func parseCapture(path string, maxGap time.Duration) ([]replayItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	dec := json.NewDecoder(file)
	var items []replayItem
	var prev time.Time
	for {
		var rec captureRecord
		if err := dec.Decode(&rec); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode capture record: %w", err)
		}
		if rec.Direction != "receive" {
			continue
		}
		message := rec.Message
		if message == "" && rec.Frame != nil {
			raw, err := json.Marshal(rec.Frame)
			if err != nil {
				return nil, fmt.Errorf("marshal capture frame: %w", err)
			}
			message = string(raw)
		}
		if message == "" {
			continue
		}
		var delay time.Duration
		if !prev.IsZero() && !rec.At.IsZero() {
			delay = rec.At.Sub(prev)
			if delay < 0 {
				delay = 0
			}
			if maxGap > 0 && delay > maxGap {
				delay = maxGap
			}
		}
		if !rec.At.IsZero() {
			prev = rec.At
		}
		items = append(items, replayItem{delay: delay, message: message})
	}
	return items, nil
}
