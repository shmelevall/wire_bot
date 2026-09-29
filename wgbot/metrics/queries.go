package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// VM is a thin VictoriaMetrics query client.
type VM struct {
	BaseURL string
	http    *http.Client
}

// NewVM creates a query client.
func NewVM(baseURL string) *VM {
	return &VM{BaseURL: baseURL, http: &http.Client{Timeout: 15 * time.Second}}
}

// Point is one (time, value) sample.
type Point struct {
	T time.Time
	V float64
}

// Sample is a labeled instant value.
type Sample struct {
	Labels map[string]string
	Value  float64
}

// QueryRange runs a range query. step is the resolution; the query itself
// should use a window (e.g. increase(...[1h])) matching it.
func (v *VM) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]Point, error) {
	q := url.Values{}
	q.Set("query", query)
	q.Set("start", start.Format(time.RFC3339))
	q.Set("end", end.Format(time.RFC3339))
	q.Set("step", strconv.FormatInt(int64(step.Seconds()), 10)+"s")

	body, err := v.get(ctx, "/api/v1/query_range", q)
	if err != nil {
		return nil, err
	}
	var r struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Values [][]json.Number `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("VM status %q", r.Status)
	}
	var pts []Point
	for _, res := range r.Data.Result {
		for _, pair := range res.Values {
			if len(pair) != 2 {
				continue
			}
			ts, err1 := pair[0].Int64()
			fv, err2 := pair[1].Float64()
			if err1 != nil || err2 != nil {
				continue
			}
			pts = append(pts, Point{T: time.Unix(ts, 0), V: fv})
		}
	}
	return pts, nil
}

// QueryInstant runs an instant query and returns labeled samples.
func (v *VM) QueryInstant(ctx context.Context, query string) ([]Sample, error) {
	q := url.Values{}
	q.Set("query", query)
	body, err := v.get(ctx, "/api/v1/query", q)
	if err != nil {
		return nil, err
	}
	var r struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []json.Number     `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("VM status %q", r.Status)
	}
	var out []Sample
	for _, res := range r.Data.Result {
		if len(res.Value) != 2 {
			continue
		}
		fv, err := res.Value[1].Float64()
		if err != nil {
			continue
		}
		out = append(out, Sample{Labels: res.Metric, Value: fv})
	}
	return out, nil
}

func (v *VM) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.BaseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("VM %s: HTTP %d", path, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
