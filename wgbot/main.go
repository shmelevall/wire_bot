// wgbot: Telegram bot for managing a WireGuard server installed by the
// Nyr wireguard-install script. Collects traffic metrics into VictoriaMetrics.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"wgbot/bot"
	"wgbot/client"
	"wgbot/metrics"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	mgr := client.New(cfg.WGConfPath, cfg.WGIface, cfg.ClientsDir)
	if _, err := mgr.List(); err != nil {
		fmt.Fprintln(os.Stderr, "cannot read WireGuard config:", err)
		fmt.Fprintln(os.Stderr, "run wireguard-install.sh first")
		os.Exit(1)
	}

	vm := metrics.NewVM(cfg.VMURL)
	b, err := bot.New(bot.Config{
		Token:         cfg.Token,
		AdminIDs:      cfg.AdminIDs,
		SummaryChatID: cfg.SummaryChatID,
		SummaryTime:   cfg.SummaryTime,
	}, mgr, vm)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bot init:", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Metrics collector: `wg show` -> VictoriaMetrics every 30s.
	collector := metrics.NewCollector(cfg.VMURL, cfg.WGIface, func() *client.Manager { return mgr })
	go collector.Start(ctx)

	// Daily summary scheduler.
	go b.RunSummaryScheduler(ctx)

	fmt.Println("wgbot started")
	b.Start(ctx) // blocking
	fmt.Println("wgbot stopped")
}
