package bot

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	tbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"wgbot/charts"
	"wgbot/metrics"
)

func chartsTraffic(name, period string, rx, tx []metrics.Point) ([]byte, error) {
	return charts.Traffic(fmt.Sprintf("Трафик клиента: %s (%s)", name, period), rx, tx)
}

// RunSummaryScheduler sends a daily summary chart at the configured local time.
func (b *Bot) RunSummaryScheduler(ctx context.Context) {
	hh, mm := 9, 0
	if parts := strings.Split(b.summaryTime, ":"); len(parts) == 2 {
		if v, err := strconv.Atoi(parts[0]); err == nil && v >= 0 && v < 24 {
			hh = v
		}
		if v, err := strconv.Atoi(parts[1]); err == nil && v >= 0 && v < 60 {
			mm = v
		}
	}
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		log.Printf("[summary] next run at %s", next.Format(time.RFC3339))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			b.sendSummaryTo(ctx, b.summaryChat)
		}
	}
}

// sendSummary sends the daily summary immediately (used by /summary).
func (b *Bot) sendSummary(ctx context.Context, chatID int64) {
	b.sendSummaryTo(ctx, chatID)
}

// sendSummaryTo builds the daily summary: a per-client usage bar chart plus
// a text digest (traffic totals and who was active during the last 24h).
func (b *Bot) sendSummaryTo(ctx context.Context, chatID int64) {
	if chatID == 0 {
		return
	}
	samples, err := b.vm.QueryInstant(ctx,
		`sum by (client, dir) (increase(wg_client_transfer_bytes_total[24h]))`)
	if err != nil {
		b.send(ctx, chatID, "Сводка недоступна (ошибка VictoriaMetrics: "+err.Error()+")")
		return
	}
	usage := map[string]float64{}
	rxBy := map[string]float64{}
	txBy := map[string]float64{}
	for _, s := range samples {
		name, dir := s.Labels["client"], s.Labels["dir"]
		if name == "" {
			continue
		}
		switch dir {
		case "rx":
			rxBy[name] += s.Value
		case "tx":
			txBy[name] += s.Value
		}
		usage[name] += s.Value
	}

	// Activity: last handshake time per client (from VM) and the live interface.
	lastSeen := map[string]time.Time{}
	if hs, err := b.vm.QueryInstant(ctx, `max by (client) (wg_client_last_handshake_seconds)`); err == nil {
		for _, s := range hs {
			name := s.Labels["client"]
			if name != "" && s.Value > 0 {
				lastSeen[name] = time.Unix(int64(s.Value), 0)
			}
		}
	}
	// Prefer the live interface data (fresher than the 30s polling interval).
	if clients, err := b.mgr.List(); err == nil {
		for pub, t := range b.lastHandshakes() {
			for _, c := range clients {
				if c.PublicKey == pub {
					if t.After(lastSeen[c.Name]) {
						lastSeen[c.Name] = t
					}
				}
			}
		}
	}
	var active []string
	for name, t := range lastSeen {
		if time.Since(t) < 24*time.Hour {
			active = append(active, name)
		}
	}
	sort.Strings(active)

	var sb strings.Builder
	sb.WriteString("📅 Суточный саммари WireGuard (24ч)\n\n")
	if len(usage) == 0 {
		sb.WriteString("Трафика за последние сутки не зафиксировано.")
	} else {
		names := make([]string, 0, len(usage))
		for n := range usage {
			names = append(names, n)
		}
		sort.Slice(names, func(i, j int) bool { return usage[names[i]] > usage[names[j]] })
		for _, n := range names {
			sb.WriteString(fmt.Sprintf("%s — RX %s / TX %s (всего %s), %s\n",
				n, metrics.HumanBytes(rxBy[n]), metrics.HumanBytes(txBy[n]),
				metrics.HumanBytes(usage[n]), lastSeenText(lastSeen[n])))
		}
		var total float64
		for _, v := range usage {
			total += v
		}
		sb.WriteString(fmt.Sprintf("\nВсего: %s\nАктивны за 24ч: %s",
			metrics.HumanBytes(total), strings.Join(active, ", ")))
	}

	png, err := charts.UsageBars("Трафик по клиентам за 24 часа", usage)
	if err != nil {
		b.send(ctx, chatID, sb.String())
		return
	}
	if png == nil {
		b.send(ctx, chatID, sb.String())
		return
	}
	if _, err := b.api.SendPhoto(ctx, &tbot.SendPhotoParams{
		ChatID:  chatID,
		Photo:   &models.InputFileUpload{Filename: "summary.png", Data: bytes.NewReader(png)},
		Caption: sb.String(),
	}); err != nil {
		log.Printf("[bot] summary photo: %v", err)
		b.send(ctx, chatID, sb.String())
	}
}
