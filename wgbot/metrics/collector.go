// Package metrics collects wg transfer counters into VictoriaMetrics and
// queries them back for charts.
package metrics

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wgbot/client"
	"wgbot/wireguard"
)

// Collector polls `wg show <iface> dump` and pushes counters to VM.
type Collector struct {
	VMURL  string
	Iface  string
	Client func() *client.Manager

	interval time.Duration
	http     *http.Client
}

// NewCollector creates a collector with a 30s polling interval.
func NewCollector(vmURL, iface string, mgr func() *client.Manager) *Collector {
	return &Collector{
		VMURL:    strings.TrimRight(vmURL, "/"),
		Iface:    iface,
		Client:   mgr,
		interval: 30 * time.Second,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Start runs the collection loop until ctx is cancelled.
func (c *Collector) Start(ctx context.Context) {
	// First sample immediately so data exists right after start.
	c.collect(ctx)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.collect(ctx)
		}
	}
}

func (c *Collector) collect(ctx context.Context) {
	stats, err := wireguard.Dump(c.Iface)
	if err != nil {
		log.Printf("[collector] wg dump: %v", err)
		return
	}
	mgr := c.Client()
	cl, err := mgr.List()
	if err != nil {
		log.Printf("[collector] load clients: %v", err)
		return
	}
	byPub := map[string]string{}
	for _, cli := range cl {
		byPub[cli.PublicKey] = cli.Name
	}

	var buf bytes.Buffer
	nowMs := time.Now().UnixMilli()
	for _, s := range stats {
		name, ok := byPub[s.PublicKey]
		if !ok {
			continue // not one of our managed peers
		}
		fmt.Fprintf(&buf, "wg_client_transfer_bytes_total{client=%q,dir=\"rx\"} %d %d\n",
			name, s.Rx, nowMs)
		fmt.Fprintf(&buf, "wg_client_transfer_bytes_total{client=%q,dir=\"tx\"} %d %d\n",
			name, s.Tx, nowMs)
		fmt.Fprintf(&buf, "wg_client_last_handshake_seconds{client=%q} %d %d\n",
			name, s.LastHandshake.Unix(), nowMs)
	}
	if buf.Len() == 0 {
		return
	}
	url := c.VMURL + "/api/v1/import/prometheus"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return
	}
	resp, err := c.http.Do(req)
	if err != nil {
		log.Printf("[collector] push to VM: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		log.Printf("[collector] VM push status %d", resp.StatusCode)
	}
}

// HumanBytes formats bytes for humans.
func HumanBytes(b float64) string {
	const unit = 1024
	if b < unit {
		return strconv.FormatFloat(b, 'f', 0, 64) + " B"
	}
	div, exp := float64(unit), 0
	for n := b / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", b/div, "KMGTPE"[exp])
}
