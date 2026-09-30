// Package bot implements the Telegram user interface.
package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	tbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"wgbot/client"
	"wgbot/metrics"
)

// Config is the bot configuration.
type Config struct {
	Token         string
	AdminIDs      []int64
	SummaryChatID int64
	SummaryTime   string // "HH:MM" local time
}

// Bot wires the Telegram API to the client manager and metrics.
type Bot struct {
	api         *tbot.Bot
	mgr         *client.Manager
	vm          *metrics.VM
	admins      map[int64]struct{}
	summaryChat int64
	summaryTime string

	mu     sync.Mutex
	states map[int64]*userState
}

type userState struct {
	step string // "name" | "dns" | "custom_dns"
	name string
}

// DNS option labels, mirroring the reference script's new_client_dns().
var dnsLabels = []string{
	"1) Системные резолверы",
	"2) Google",
	"3) 1.1.1.1",
	"4) OpenDNS",
	"5) Quad9",
	"6) Gcore",
	"7) AdGuard",
	"8) Свои резолверы",
}

// New creates the Bot.
func New(cfg Config, mgr *client.Manager, vm *metrics.VM) (*Bot, error) {
	b := &Bot{
		mgr:         mgr,
		vm:          vm,
		admins:      map[int64]struct{}{},
		summaryChat: cfg.SummaryChatID,
		summaryTime: cfg.SummaryTime,
		states:      map[int64]*userState{},
	}
	for _, id := range cfg.AdminIDs {
		b.admins[id] = struct{}{}
	}
	api, err := tbot.New(cfg.Token, tbot.WithDefaultHandler(b.handle))
	if err != nil {
		return nil, err
	}
	b.api = api
	return b, nil
}

// Start runs the long-polling loop (blocking).
func (b *Bot) Start(ctx context.Context) {
	b.api.Start(ctx)
}

// --- helpers ---

func (b *Bot) isAdmin(id int64) bool {
	_, ok := b.admins[id]
	return ok
}

func (b *Bot) send(ctx context.Context, chatID int64, text string) {
	_, err := b.api.SendMessage(ctx, &tbot.SendMessageParams{ChatID: chatID, Text: text})
	if err != nil {
		log.Printf("[bot] send: %v", err)
	}
}

func (b *Bot) sendWithKeyboard(ctx context.Context, chatID int64, text string, kb models.InlineKeyboardMarkup) {
	_, err := b.api.SendMessage(ctx, &tbot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: kb,
	})
	if err != nil {
		log.Printf("[bot] send: %v", err)
	}
}

// ask sends a prompt that forces a text reply. In group chats bots run in
// privacy mode and only receive commands and replies to their own messages,
// so force_reply is required for the dialog to work there.
func (b *Bot) ask(ctx context.Context, chatID int64, text string) {
	_, err := b.api.SendMessage(ctx, &tbot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
		ReplyMarkup: models.ForceReply{
			ForceReply:            true,
			InputFieldPlaceholder: "введите ответ",
		},
	})
	if err != nil {
		log.Printf("[bot] send: %v", err)
	}
}

func inlineKB(rows ...[]models.InlineKeyboardButton) models.InlineKeyboardMarkup {
	return models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func btn(text, data string) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, CallbackData: data}
}

func (b *Bot) getState(userID int64) *userState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.states[userID]
}

func (b *Bot) setState(userID int64, s *userState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.states[userID] = s
}

func (b *Bot) clearState(userID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.states, userID)
}

// --- main update handler ---

func (b *Bot) handle(ctx context.Context, api *tbot.Bot, update *models.Update) {
	if update.Message != nil && update.Message.From != nil {
		log.Printf("[fsm] message from=%d chat=%d text=%q",
			update.Message.From.ID, update.Message.Chat.ID, update.Message.Text)
		if !b.isAdmin(update.Message.From.ID) {
			log.Printf("[fsm] user %d is not an admin, ignored", update.Message.From.ID)
			return
		}
		b.onMessage(ctx, update.Message)
		return
	}
	if update.CallbackQuery != nil {
		log.Printf("[fsm] callback from=%d data=%q", update.CallbackQuery.From.ID, update.CallbackQuery.Data)
		if !b.isAdmin(update.CallbackQuery.From.ID) {
			return
		}
		b.onCallback(ctx, update.CallbackQuery)
	}
}

const helpText = `🤖 wgbot — управление WireGuard

/add — добавить клиента (имя → выбор DNS → конфиг + QR)
/list — список клиентов (блокировка, удаление, статистика)
/stats — статистика трафика клиента за период
/summary — суточный саммари прямо сейчас
/help — эта справка`

func (b *Bot) onMessage(ctx context.Context, msg *models.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := strings.TrimSpace(msg.Text)

	switch {
	case text == "/start" || text == "/help":
		b.send(ctx, chatID, helpText)
	case text == "/add":
		b.setState(userID, &userState{step: "name"})
		b.ask(ctx, chatID, "Введите имя нового клиента (латиница, цифры, «-», «_», до 15 символов):")
	case text == "/list":
		b.sendList(ctx, chatID)
	case text == "/stats":
		b.sendStatsSelector(ctx, chatID)
	case text == "/summary":
		b.sendSummary(ctx, chatID)
	default:
		st := b.getState(userID)
		if st == nil {
			b.send(ctx, chatID, "Неизвестная команда. /help — список команд.")
			return
		}
		b.onStateInput(ctx, chatID, userID, st, text)
	}
}

func (b *Bot) onStateInput(ctx context.Context, chatID, userID int64, st *userState, text string) {
	log.Printf("[fsm] state input user=%d step=%s name=%q text=%q", userID, st.step, st.name, text)
	switch st.step {
	case "name":
		if err := client.ValidName(text); err != nil {
			b.ask(ctx, chatID, err.Error()+"\nПопробуйте ещё раз:")
			return
		}
		if _, err := b.mgr.List(); err != nil {
			b.send(ctx, chatID, "Ошибка чтения конфигурации: "+err.Error())
			b.clearState(userID)
			return
		}
		st.name = text
		st.step = "dns"
		b.sendDNSChoice(ctx, chatID)
	case "custom_dns":
		dns := parseCustomDNS(text)
		if dns == "" {
			b.ask(ctx, chatID, "Некорректный ввод. Введите один или несколько IPv4-адресов через запятую:")
			return
		}
		b.createClient(ctx, chatID, userID, st.name, dns)
	default:
		b.send(ctx, chatID, "Неизвестная команда. /help — список команд.")
	}
}

func (b *Bot) sendDNSChoice(ctx context.Context, chatID int64) {
	choices := client.DNSChoices()
	var rows [][]models.InlineKeyboardButton
	for i, label := range dnsLabels {
		if choices[i] != "" {
			label += " — " + choices[i]
		}
		rows = append(rows, []models.InlineKeyboardButton{btn(label, fmt.Sprintf("dns:%d", i+1))})
	}
	b.sendWithKeyboard(ctx, chatID, "Выберите DNS-сервер для клиента:", inlineKB(rows...))
}

func parseCustomDNS(input string) string {
	var out []string
	for _, part := range strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == ' ' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		octets := strings.Split(part, ".")
		valid := len(octets) == 4
		for _, o := range octets {
			if n, err := strconv.Atoi(o); err != nil || n < 0 || n > 255 {
				valid = false
			}
		}
		if valid {
			out = append(out, part)
		}
	}
	return strings.Join(out, ", ")
}
