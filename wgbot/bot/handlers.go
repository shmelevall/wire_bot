package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"wgbot/client"
	"wgbot/qr"
	"wgbot/wireguard"
)

func (b *Bot) createClient(ctx context.Context, chatID, userID int64, name, dns string) {
	b.clearState(userID)
	path, content, err := b.mgr.Add(name, dns)
	if err != nil {
		b.send(ctx, chatID, "Ошибка создания клиента: "+err.Error())
		return
	}
	_, err = b.api.SendDocument(ctx, &tbot.SendDocumentParams{
		ChatID:   chatID,
		Document: &models.InputFileUpload{Filename: name + ".conf", Data: strings.NewReader(content)},
		Caption:  fmt.Sprintf("Конфигурация клиента %s (сохранена: %s)", name, path),
	})
	if err != nil {
		log.Printf("[bot] send document: %v", err)
	}
	png, err := qr.PNG(content)
	if err == nil {
		if _, err := b.api.SendPhoto(ctx, &tbot.SendPhotoParams{
			ChatID:  chatID,
			Photo:   &models.InputFileUpload{Filename: name + ".png", Data: strings.NewReader(string(png))},
			Caption: fmt.Sprintf("QR-код для подключения клиента %s", name),
		}); err != nil {
			log.Printf("[bot] send photo: %v", err)
		}
	} else {
		log.Printf("[bot] qr: %v", err)
	}
	b.send(ctx, chatID, fmt.Sprintf("✅ Клиент %s добавлен (без перезапуска WireGuard).", name))
}

// lastHandshakes returns a publicKey -> last handshake time map from the
// live interface (blocked peers are absent from the live interface).
func (b *Bot) lastHandshakes() map[string]time.Time {
	out := map[string]time.Time{}
	stats, err := wireguard.Dump(b.mgr.Iface)
	if err != nil {
		return out
	}
	for _, s := range stats {
		out[s.PublicKey] = s.LastHandshake
	}
	return out
}

// sendList shows all clients with per-client buttons.
func (b *Bot) sendList(ctx context.Context, chatID int64) {
	clients, err := b.mgr.List()
	if err != nil {
		b.send(ctx, chatID, "Ошибка: "+err.Error())
		return
	}
	if len(clients) == 0 {
		b.send(ctx, chatID, "Клиентов нет. Добавьте через /add.")
		return
	}
	hs := b.lastHandshakes()
	var rows [][]models.InlineKeyboardButton
	var sb strings.Builder
	sb.WriteString("Клиенты WireGuard:\n\n")
	for i, c := range clients {
		status := "🟢 " + lastSeenText(hs[c.PublicKey])
		if c.Blocked {
			status = "🔴 заблокирован"
		}
		sb.WriteString(fmt.Sprintf("%d. %s — %s (%s)\n", i+1, c.Name, status, c.AllowedIPs))
		rows = append(rows, []models.InlineKeyboardButton{btn(c.Name, "menu:"+c.Name)})
	}
	b.sendWithKeyboard(ctx, chatID, sb.String(), inlineKB(rows...))
}

func (b *Bot) sendClientMenu(ctx context.Context, chatID int64, name string) {
	clients, err := b.mgr.List()
	if err != nil {
		b.send(ctx, chatID, "Ошибка: "+err.Error())
		return
	}
	var c *client.Client
	for i := range clients {
		if clients[i].Name == name {
			c = &clients[i]
		}
	}
	if c == nil {
		b.send(ctx, chatID, "Клиент не найден.")
		return
	}
	blockBtn := btn("🔒 Заблокировать", "block:"+name)
	status := "🟢 активен, " + lastSeenText(b.lastHandshakes()[c.PublicKey])
	if c.Blocked {
		blockBtn = btn("🔓 Разблокировать", "unblock:"+name)
		status = "🔴 заблокирован"
	}
	rows := [][]models.InlineKeyboardButton{
		{blockBtn},
		{btn("🗑 Удалить", "del:"+name)},
		{btn("📊 Статистика", "stats:"+name)},
	}
	b.sendWithKeyboard(ctx, chatID,
		fmt.Sprintf("Клиент %s — %s\nIP: %s", name, status, c.AllowedIPs), inlineKB(rows...))
}

func (b *Bot) sendStatsSelector(ctx context.Context, chatID int64) {
	clients, err := b.mgr.List()
	if err != nil {
		b.send(ctx, chatID, "Ошибка: "+err.Error())
		return
	}
	if len(clients) == 0 {
		b.send(ctx, chatID, "Клиентов нет.")
		return
	}
	var rows [][]models.InlineKeyboardButton
	for _, c := range clients {
		rows = append(rows, []models.InlineKeyboardButton{btn(c.Name, "stats:"+c.Name)})
	}
	b.sendWithKeyboard(ctx, chatID, "Статистика по клиенту:", inlineKB(rows...))
}

func (b *Bot) sendPeriodSelector(ctx context.Context, chatID int64, name string) {
	rows := [][]models.InlineKeyboardButton{{
		btn("24 часа", "per:"+name+":24h"),
		btn("7 дней", "per:"+name+":7d"),
		btn("30 дней", "per:"+name+":30d"),
	}}
	b.sendWithKeyboard(ctx, chatID, "Период статистики для "+name+":", inlineKB(rows...))
}
