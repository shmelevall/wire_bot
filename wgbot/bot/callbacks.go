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
	"wgbot/metrics"
)

// sendClientStats builds and sends the traffic chart for a client.
func (b *Bot) sendClientStats(ctx context.Context, chatID int64, name, period string) {
	dur, err := time.ParseDuration(period)
	if err != nil {
		dur = 24 * time.Hour
	}
	end := time.Now().Truncate(time.Minute)
	start := end.Add(-dur)
	step := 10 * time.Minute
	if dur > 48*time.Hour {
		step = time.Hour
	}
	if dur > 8*24*time.Hour {
		step = 3 * time.Hour
	}
	window := step.String()

	q := func(dir string) string {
		return fmt.Sprintf(`increase(wg_client_transfer_bytes_total{client=%q,dir=%q}[%s])`, name, dir, window)
	}
	rx, errRx := b.vm.QueryRange(ctx, q("rx"), start, end, step)
	tx, errTx := b.vm.QueryRange(ctx, q("tx"), start, end, step)
	if errRx != nil || errTx != nil {
		b.send(ctx, chatID, "Ошибка запроса метрик (VictoriaMetrics доступна?)")
		return
	}
	png, err := chartsTraffic(name, period, rx, tx)
	if png == nil {
		b.send(ctx, chatID, "За выбранный период нет данных о трафике клиента "+name+".")
		return
	}
	if err != nil {
		b.send(ctx, chatID, "Ошибка построения графика: "+err.Error())
		return
	}
	var rxSum, txSum float64
	for _, p := range rx {
		rxSum += p.V
	}
	for _, p := range tx {
		txSum += p.V
	}
	caption := fmt.Sprintf("📊 %s — %s\nRX: %s, TX: %s",
		name, period, metrics.HumanBytes(rxSum), metrics.HumanBytes(txSum))
	if _, err := b.api.SendPhoto(ctx, &tbot.SendPhotoParams{
		ChatID:  chatID,
		Photo:   &models.InputFileUpload{Filename: name + "-stats.png", Data: strings.NewReader(string(png))},
		Caption: caption,
	}); err != nil {
		log.Printf("[bot] send stats photo: %v", err)
	}
}

func (b *Bot) onCallback(ctx context.Context, cb *models.CallbackQuery) {
	chatID := int64(0)
	if cb.Message.Message != nil {
		chatID = cb.Message.Message.Chat.ID
	} else if cb.Message.InaccessibleMessage != nil {
		chatID = cb.Message.InaccessibleMessage.Chat.ID
	}
	if chatID == 0 {
		return
	}
	b.api.AnswerCallbackQuery(ctx, &tbot.AnswerCallbackQueryParams{CallbackQueryID: cb.ID})

	data := cb.Data
	switch {
	case strings.HasPrefix(data, "dns:"):
		b.onDNSCallback(ctx, chatID, cb, strings.TrimPrefix(data, "dns:"))
	case strings.HasPrefix(data, "menu:"):
		b.sendClientMenu(ctx, chatID, strings.TrimPrefix(data, "menu:"))
	case strings.HasPrefix(data, "block:"):
		name := strings.TrimPrefix(data, "block:")
		if err := b.mgr.Block(name); err != nil {
			b.send(ctx, chatID, "Ошибка: "+err.Error())
		} else {
			b.send(ctx, chatID, "🔒 Клиент "+name+" заблокирован.")
		}
	case strings.HasPrefix(data, "unblock:"):
		name := strings.TrimPrefix(data, "unblock:")
		if err := b.mgr.Unblock(name); err != nil {
			b.send(ctx, chatID, "Ошибка: "+err.Error())
		} else {
			b.send(ctx, chatID, "🔓 Клиент "+name+" разблокирован.")
		}
	case strings.HasPrefix(data, "del:"):
		name := strings.TrimPrefix(data, "del:")
		b.sendWithKeyboard(ctx, chatID, "Удалить клиента "+name+"? Отменить будет нельзя.",
			inlineKB([]models.InlineKeyboardButton{btn("Да, удалить", "delok:"+name)}))
	case strings.HasPrefix(data, "delok:"):
		name := strings.TrimPrefix(data, "delok:")
		if err := b.mgr.Delete(name); err != nil {
			b.send(ctx, chatID, "Ошибка: "+err.Error())
		} else {
			b.send(ctx, chatID, "🗑 Клиент "+name+" удалён.")
		}
	case strings.HasPrefix(data, "stats:"):
		b.sendPeriodSelector(ctx, chatID, strings.TrimPrefix(data, "stats:"))
	case strings.HasPrefix(data, "per:"):
		parts := strings.Split(strings.TrimPrefix(data, "per:"), ":")
		if len(parts) == 2 {
			b.sendClientStats(ctx, chatID, parts[0], parts[1])
		}
	}
}

func (b *Bot) onDNSCallback(ctx context.Context, chatID int64, cb *models.CallbackQuery, choice string) {
	userID := cb.From.ID
	st := b.getState(userID)
	if st == nil || st.step != "dns" {
		b.send(ctx, chatID, "Сессия устарела. Начните заново с /add.")
		return
	}
	if choice == "8" {
		st.step = "custom_dns"
		b.send(ctx, chatID, "Введите DNS-серверы (IPv4, через запятую или пробел):")
		return
	}
	idx := 0
	fmt.Sscanf(choice, "%d", &idx)
	choices := client.DNSChoices()
	if idx < 1 || idx > 7 {
		return
	}
	b.createClient(ctx, chatID, userID, st.name, choices[idx-1])
}
