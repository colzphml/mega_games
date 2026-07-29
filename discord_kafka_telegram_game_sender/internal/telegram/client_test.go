package telegram

import (
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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
	form   *multipart.Form
}

// telegramStub answers getMe so NewWithEndpoint - which calls it
// synchronously through tgbotapi.NewBotAPIWithClient - can construct the
// client, then records the next request into got. sendPhoto goes out as
// multipart/form-data (the library streams the image as a file part), not
// as a plain url-encoded form.
//
// This is a real net/http server, not a mock of the tgbotapi package: the
// assertions in the tests below check what actually left the process on
// the wire (chat_id, caption, the uploaded photo itself) rather than what
// a mock remembers being told to send.
func telegramStub(t *testing.T, got *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"username":"test_bot"}}`))
			return
		}

		got.sent = true
		got.method = r.Method
		got.path = r.URL.Path
		if err := r.ParseMultipartForm(32 << 20); err == nil {
			got.form = r.MultipartForm
		}

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

func formValue(form *multipart.Form, key string) string {
	values := form.Value[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func TestSendGameImageSendsCaptionAndPhotoToConfiguredChat(t *testing.T) {
	var got capturedRequest
	srv := telegramStub(t, &got)
	defer srv.Close()

	client := newTestClient(t, srv)

	image := []byte("not-really-a-png-but-the-stub-does-not-decode-it")
	caption := "Falcons 24 - 20 Bucs"
	if err := client.SendGameImage(context.Background(), caption, image); err != nil {
		t.Fatalf("send: %v", err)
	}

	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}
	if !strings.HasSuffix(got.path, "/sendPhoto") {
		t.Errorf("path = %q, want a call to sendPhoto", got.path)
	}
	if got.form == nil {
		t.Fatal("request body was not valid multipart/form-data")
	}
	if chatID := formValue(got.form, "chat_id"); chatID != "1" {
		t.Errorf("chat_id = %q, want the configured chat 1", chatID)
	}
	if gotCaption := formValue(got.form, "caption"); gotCaption != caption {
		t.Errorf("caption = %q, want %q", gotCaption, caption)
	}
	files := got.form.File["photo"]
	if len(files) != 1 {
		t.Fatalf("photo file parts = %d, want exactly 1", len(files))
	}
	if files[0].Size != int64(len(image)) {
		t.Errorf("uploaded photo size = %d bytes, want %d", files[0].Size, len(image))
	}
}

func TestSendGameImageRejectsOversizedPhotoBeforeSending(t *testing.T) {
	var got capturedRequest
	srv := telegramStub(t, &got)
	defer srv.Close()

	client := newTestClient(t, srv)

	oversized := make([]byte, 11<<20) // Telegram rejects sendPhoto above 10 MB
	err := client.SendGameImage(context.Background(), "caption", oversized)
	if err == nil {
		t.Error("an oversized photo must fail before the request goes out")
	}
	if got.sent {
		t.Error("an oversized photo must be rejected locally, not sent to Telegram and rejected there " +
			"(that's ten wasted retries against a request that can never succeed)")
	}
}

func TestSendGameImageAllowsPhotoExactlyAtTheLimit(t *testing.T) {
	var got capturedRequest
	srv := telegramStub(t, &got)
	defer srv.Close()

	client := newTestClient(t, srv)

	atLimit := make([]byte, 10<<20) // exactly the limit must still be sent, not rejected
	if err := client.SendGameImage(context.Background(), "caption", atLimit); err != nil {
		t.Fatalf("a photo exactly at the limit must be sent, not rejected: %v", err)
	}
	if !got.sent {
		t.Error("expected a sendPhoto request to reach the server")
	}
}

func TestSendGameImageRespectsCancelledContext(t *testing.T) {
	var got capturedRequest
	srv := telegramStub(t, &got)
	defer srv.Close()

	client := newTestClient(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.SendGameImage(ctx, "caption", []byte("small image")); err == nil {
		t.Error("a cancelled context must abort the send")
	}
	if got.sent {
		t.Error("a cancelled context must prevent any sendPhoto request from going out")
	}
}
