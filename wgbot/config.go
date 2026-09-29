package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime settings, sourced from environment variables
// (written by wgbot-install.sh into /etc/wgbot/wgbot.env).
type Config struct {
	Token         string
	AdminIDs      []int64
	SummaryChatID int64
	SummaryTime   string
	VMURL         string
	WGConfPath    string
	WGIface       string
	ClientsDir    string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// LoadConfig reads the configuration from the environment.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Token:       env("WGBOT_TOKEN", ""),
		SummaryTime: env("WGBOT_SUMMARY_TIME", "09:00"),
		VMURL:       env("WGBOT_VM_URL", "http://127.0.0.1:8428"),
		WGConfPath:  env("WGBOT_WG_CONF", "/etc/wireguard/wg0.conf"),
		WGIface:     env("WGBOT_WG_IFACE", "wg0"),
		ClientsDir:  env("WGBOT_CLIENTS_DIR", "/etc/wireguard/clients"),
	}

	if cfg.Token == "" {
		return nil, fmt.Errorf("WGBOT_TOKEN не задан")
	}
	for _, idStr := range strings.Split(env("WGBOT_ADMIN_IDS", ""), ",") {
		idStr = strings.TrimSpace(idStr)
		if idStr == "" {
			continue
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("некорректный ID админа %q", idStr)
		}
		cfg.AdminIDs = append(cfg.AdminIDs, id)
	}
	if len(cfg.AdminIDs) == 0 {
		return nil, fmt.Errorf("WGBOT_ADMIN_IDS не задан (список Telegram user ID через запятую)")
	}
	if v := env("WGBOT_SUMMARY_CHAT_ID", ""); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("некорректный WGBOT_SUMMARY_CHAT_ID %q", v)
		}
		cfg.SummaryChatID = id
	} else {
		// default: the first admin gets the daily summary
		cfg.SummaryChatID = cfg.AdminIDs[0]
	}
	return cfg, nil
}
