// Package charts renders PNG charts from metric points using go-chart.
package charts

import (
	"bytes"
	"fmt"
	"sort"
	"time"

	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"

	"wgbot/metrics"
)

// Traffic renders an RX/TX line chart for a single client.
func Traffic(title string, rx, tx []metrics.Point) ([]byte, error) {
	var series []chart.Series
	if len(rx) > 1 {
		series = append(series, timeSeries("RX (получено)", rx, drawing.ColorBlue))
	}
	if len(tx) > 1 {
		series = append(series, timeSeries("TX (отправлено)", tx, drawing.ColorGreen))
	}
	if len(series) == 0 {
		return nil, nil // not enough data
	}
	graph := chart.Chart{
		Title: title,
		XAxis: chart.XAxis{
			ValueFormatter: chart.TimeValueFormatterWithFormat("2006-01-02 15:04"),
		},
		YAxis: chart.YAxis{
			Name:           "байт",
			ValueFormatter: byteValueFormatter,
		},
		Series: series,
	}
	graph.Elements = []chart.Renderable{chart.Legend(&graph)}
	var buf bytes.Buffer
	if err := graph.Render(chart.PNG, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UsageBars renders a bar chart of per-client traffic totals.
func UsageBars(title string, usage map[string]float64) ([]byte, error) {
	if len(usage) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(usage))
	for name := range usage {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return usage[names[i]] > usage[names[j]] })

	bars := make([]chart.Value, 0, len(names))
	for _, name := range names {
		bars = append(bars, chart.Value{Label: name, Value: usage[name]})
	}
	graph := chart.BarChart{
		Title:  title,
		Height: 200 + 40*len(bars),
		Bars:   bars,
		YAxis: chart.YAxis{
			ValueFormatter: byteValueFormatter,
		},
	}
	var buf bytes.Buffer
	if err := graph.Render(chart.PNG, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// byteValueFormatter formats axis values in human-readable byte units.
func byteValueFormatter(v interface{}) string {
	f, ok := v.(float64)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return metrics.HumanBytes(f)
}

func timeSeries(name string, pts []metrics.Point, color drawing.Color) chart.Series {
	xs := make([]time.Time, len(pts))
	ys := make([]float64, len(pts))
	for i, p := range pts {
		xs[i] = p.T
		ys[i] = p.V
	}
	return chart.TimeSeries{
		Name:    name,
		XValues: xs,
		YValues: ys,
		Style: chart.Style{
			StrokeColor: color,
			FillColor:   color.WithAlpha(50),
		},
	}
}
