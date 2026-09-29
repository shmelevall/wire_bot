package charts

import (
	"testing"
	"time"

	"wgbot/metrics"
)

func TestTrafficRendersPNG(t *testing.T) {
	now := time.Now()
	var rx, tx []metrics.Point
	for i := 0; i < 50; i++ {
		rx = append(rx, metrics.Point{T: now.Add(time.Duration(i) * time.Minute), V: float64(i) * 1e6})
		tx = append(tx, metrics.Point{T: now.Add(time.Duration(i) * time.Minute), V: float64(i) * 5e5})
	}
	png, err := Traffic("Test client (24h)", rx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if len(png) < 5000 {
		t.Fatalf("suspiciously small PNG: %d bytes", len(png))
	}
}

func TestTrafficNoData(t *testing.T) {
	png, err := Traffic("empty", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if png != nil {
		t.Fatal("expected nil PNG for no data")
	}
}

func TestUsageBarsRendersPNG(t *testing.T) {
	png, err := UsageBars("Usage 24h", map[string]float64{
		"alice": 3.2e9, "bob": 8.1e8, "carol": 5e7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(png) < 5000 {
		t.Fatalf("suspiciously small PNG: %d bytes", len(png))
	}
}
