package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// capturedRequest holds what the stub server saw for the request under
// test, i.e. anything other than the getMe call the constructor makes.
type capturedRequest struct {
	sent   bool
	method string
	path   string
	form   url.Values
}

// telegramStub answers getMe so NewWithEndpoint - which calls it
// synchronously through tgbotapi.NewBotAPIWithClient - can construct the
// client, then records the next request's method, path and decoded form
// body into got.
//
// This is a real net/http server, not a mock of the tgbotapi package: the
// assertions in the tests below check what actually left the process on
// the wire (parse_mode, chat_id, disable_web_page_preview) rather than
// what a mock remembers being told to send.
func telegramStub(t *testing.T, got *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"username":"test_bot"}}`))
			return
		}

		_ = r.ParseForm()
		got.sent = true
		got.method = r.Method
		got.path = r.URL.Path
		got.form = r.PostForm

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":0,"chat":{"id":1,"type":"private"}}}`))
	}))
}

// newTestClient points a Client at srv the same way production code points
// one at the real Telegram API, only through NewWithEndpoint instead of
// New. See client.go for why endpoint has to look like
// srv.URL+"/bot%s/%s" rather than just srv.URL.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	client, err := NewWithEndpoint("test-token", "1", srv.URL+"/bot%s/%s", zerolog.Nop())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	return client
}

func TestSendWeekMessageSendsMarkdownV2WithPreviewsDisabled(t *testing.T) {
	var got capturedRequest
	srv := telegramStub(t, &got)
	defer srv.Close()

	client := newTestClient(t, srv)

	// Escaping/formatting is tgmarkdown's job (covered by its own tests);
	// this text stands in for whatever already-formatted string that
	// package hands to SendWeekMessage.
	text := "*Week 1*\n[ATL](t.me/some_user)"
	if err := client.SendWeekMessage(context.Background(), text); err != nil {
		t.Fatalf("send: %v", err)
	}

	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}
	if !strings.HasSuffix(got.path, "/sendMessage") {
		t.Errorf("path = %q, want a call to sendMessage", got.path)
	}
	if mode := got.form.Get("parse_mode"); mode != "MarkdownV2" {
		t.Errorf("parse_mode = %q, want MarkdownV2 (V5-11 moved off legacy Markdown)", mode)
	}
	if preview := got.form.Get("disable_web_page_preview"); preview != "true" {
		t.Errorf("disable_web_page_preview = %q, want true: an unpreviewed link must not turn into a big card", preview)
	}
	if chatID := got.form.Get("chat_id"); chatID != "1" {
		t.Errorf("chat_id = %q, want the configured chat 1", chatID)
	}
	if gotText := got.form.Get("text"); gotText != text {
		t.Errorf("text = %q, want %q unchanged: this client must forward text verbatim, not reformat it", gotText, text)
	}
}

func TestSendWeekMessageRespectsCancelledContext(t *testing.T) {
	var got capturedRequest
	srv := telegramStub(t, &got)
	defer srv.Close()

	client := newTestClient(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.SendWeekMessage(ctx, "hello"); err == nil {
		t.Error("a cancelled context must abort the send")
	}
	if got.sent {
		t.Error("a cancelled context must prevent any sendMessage request from going out")
	}
}

func TestNewWithEndpointRejectsNonNumericChatID(t *testing.T) {
	var got capturedRequest
	srv := telegramStub(t, &got)
	defer srv.Close()

	if _, err := NewWithEndpoint("test-token", "not-a-number", srv.URL+"/bot%s/%s", zerolog.Nop()); err == nil {
		t.Error("a non-numeric chat id must be rejected at construction time")
	}
}
