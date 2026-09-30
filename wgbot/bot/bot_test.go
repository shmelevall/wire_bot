package bot

import (
	"context"

	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"wgbot/client"
)

const testWGConf = `# Do not alter the commented lines
# They are used by wireguard-install
# ENDPOINT 203.0.113.10

[Interface]
Address = 10.7.0.1/24
PrivateKey = GFyNmZgasNx9k0u0OPHrr7w3GXLwD8MOJaPW4/4DSmM=
ListenPort = 51820

# BEGIN_PEER alice
[Peer]
PublicKey = NjsBfSXF+isuXSZTiEMqaO0cviFbJTjdY3eLbRhDZgA=
PresharedKey = NfQvJTCLmtEm4zFEsJpyQbRxsnZZIRCCxMd2Z+vmwH4=
AllowedIPs = 10.7.0.2/32
# END_PEER alice
`

// fakeTG is a fake Telegram Bot API server capturing outgoing requests.
type fakeTG struct {
	mu       sync.Mutex
	requests []string // method names called
	texts    []string // sendMessage texts
}

func newFakeTG() (*fakeTG, *httptest.Server) {
	f := &fakeTG{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/bot1:t/")
		f.mu.Lock()
		f.requests = append(f.requests, method)
		f.mu.Unlock()
		if method == "sendMessage" {
			// The library always sends multipart/form-data.
			_ = r.ParseMultipartForm(1 << 20)
			f.mu.Lock()
			if txt := r.FormValue("text"); txt != "" {
				f.texts = append(f.texts, txt)
			}
			f.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true,"result":{"message_id":1,"date":1,"chat":{"id":1,"type":"private"}}}`)
	}))
	return f, srv
}

func newTestBot(t *testing.T, srvURL string) (*Bot, *client.Manager) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "wg0.conf")
	if err := os.WriteFile(confPath, []byte(testWGConf), 0600); err != nil {
		t.Fatal(err)
	}
	mgr := client.New(confPath, "wg0", filepath.Join(dir, "clients"))
	b := &Bot{
		mgr:    mgr,
		admins: map[int64]struct{}{100: {}},
		states: map[int64]*userState{},
	}
	api, err := tbot.New("1:t",
		tbot.WithServerURL(srvURL),
		tbot.WithSkipGetMe(),
		tbot.WithNotAsyncHandlers(), // synchronous handlers for deterministic tests
		tbot.WithDefaultHandler(b.handle))
	if err != nil {
		t.Fatal(err)
	}
	b.api = api
	return b, mgr
}

func msgUpdate(text string) *models.Update {
	return &models.Update{
		Message: &models.Message{
			Text: text,
			Chat: models.Chat{ID: 1},
			From: &models.User{ID: 100},
		},
	}
}

func cbUpdate(data string) *models.Update {
	m := &models.Message{Chat: models.Chat{ID: 1}}
	return &models.Update{
		CallbackQuery: &models.CallbackQuery{
			ID:   "1",
			From: models.User{ID: 100},
			Data: data,
			Message: models.MaybeInaccessibleMessage{
				Type:    models.MaybeInaccessibleMessageTypeMessage,
				Message: m,
			},
		},
	}
}

// TestAddDialog walks the whole /add FSM: prompt, name, DNS callback.
// It verifies that each step produces a reply (no "silence").
func TestAddDialog(t *testing.T) {
	f, srv := newFakeTG()
	defer srv.Close()
	b, _ := newTestBot(t, srv.URL)
	ctx := context.Background()

	// /add -> prompt for the name.
	b.api.ProcessUpdate(ctx, msgUpdate("/add"))
	if st := b.getState(100); st == nil || st.step != "name" {
		t.Fatalf("state after /add: %+v", b.getState(100))
	}
	if len(f.texts) != 1 || !strings.Contains(f.texts[0], "имя") {
		t.Fatalf("texts after /add: %v", f.texts)
	}

	// Name -> DNS choice keyboard must be sent.
	b.api.ProcessUpdate(ctx, msgUpdate("testclient"))
	if st := b.getState(100); st == nil || st.step != "dns" || st.name != "testclient" {
		t.Fatalf("state after name: %+v", b.getState(100))
	}
	if len(f.texts) != 2 {
		t.Fatalf("no reply after name entry (silence bug): texts=%v", f.texts)
	}
	if !strings.Contains(f.texts[1], "DNS") {
		t.Fatalf("second message is not the DNS prompt: %q", f.texts[1])
	}
}

// TestAddDialogCustomDNS covers the custom DNS input branch up to the DNS prompt.
func TestAddDialogCustomDNS(t *testing.T) {
	f, srv := newFakeTG()
	defer srv.Close()
	_ = f
	b, _ := newTestBot(t, srv.URL)
	ctx := context.Background()

	b.api.ProcessUpdate(ctx, msgUpdate("/add"))
	b.api.ProcessUpdate(ctx, msgUpdate("custom1"))
	b.api.ProcessUpdate(ctx, cbUpdate("dns:8"))
	if st := b.getState(100); st == nil || st.step != "custom_dns" {
		t.Fatalf("state after dns:8: %+v", b.getState(100))
	}
	if len(f.texts) < 3 {
		t.Fatalf("missing replies in custom DNS flow: %v", f.texts)
	}
	if !strings.Contains(f.texts[len(f.texts)-1], "DNS") {
		t.Fatalf("last text should ask for DNS servers: %q", f.texts[len(f.texts)-1])
	}
}

// TestNonAdminIgnored ensures non-admin messages never enter the FSM.
func TestNonAdminIgnored(t *testing.T) {
	_, srv := newFakeTG()
	defer srv.Close()
	b, _ := newTestBot(t, srv.URL)
	ctx := context.Background()

	b.api.ProcessUpdate(ctx, msgUpdate("/add"))
	b.api.ProcessUpdate(ctx, msgUpdate("testclient"))

	// Different user tries /add: must be ignored entirely.
	u := msgUpdate("/add")
	u.Message.From.ID = 999
	b.api.ProcessUpdate(ctx, u)
	if st := b.getState(999); st != nil {
		t.Fatal("non-admin got an FSM state")
	}
	if b.getState(100) == nil || b.getState(100).step != "dns" {
		t.Fatalf("admin state must be untouched: %+v", b.getState(100))
	}
}
