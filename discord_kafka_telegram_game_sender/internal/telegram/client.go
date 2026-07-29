package telegram

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog"
)

type Client struct {
	bot    *tgbotapi.BotAPI
	chatID int64
	log    zerolog.Logger
}

// maxPhotoBytes is Telegram's limit for sendPhoto. Rejecting an oversized
// image here turns a guaranteed HTTP 400 - and the ten retries the caller
// would otherwise burn rediscovering that - into one clear, immediate
// error. Live recap images run 1.4-1.6 MB, well under the limit.
const maxPhotoBytes = 10 << 20 // 10 MB

// New builds a client against the public Telegram Bot API.
func New(token, chatID string, log zerolog.Logger) (*Client, error) {
	return NewWithEndpoint(token, chatID, tgbotapi.APIEndpoint, log)
}

// NewWithEndpoint builds a client against an arbitrary endpoint so tests can
// assert on the request that actually leaves the process, rather than on
// what a mock was told to expect.
//
// go-telegram-bot-api v4.6.4 (the archived pre-v5 branch this project
// pins) has neither a NewBotAPIWithAPIEndpoint constructor nor a settable
// APIEndpoint field on BotAPI: MakeRequest and UploadFile always build the
// request URL from the package-level constant tgbotapi.APIEndpoint, and
// BotAPI has no field that overrides it. So redirection happens one layer
// down, through an http.RoundTripper that rewrites the scheme and host of
// every outgoing request to match endpoint and leaves the path the library
// builds (/bot<token>/<method>) untouched. endpoint is therefore expected
// to have the same "%s/%s" (token, method) template shape as
// tgbotapi.APIEndpoint, even though only its scheme and host end up used.
func NewWithEndpoint(token, chatID, endpoint string, log zerolog.Logger) (*Client, error) {
	parsedID, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse chat id: %w", err)
	}

	target, err := url.Parse(fmt.Sprintf(endpoint, "0", "getMe"))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     false,
		Protocols:             protocols,
	}
	bot, err := tgbotapi.NewBotAPIWithClient(token, &http.Client{
		Timeout:   2 * time.Minute,
		Transport: &endpointTransport{base: transport, scheme: target.Scheme, host: target.Host},
	})
	if err != nil {
		return nil, fmt.Errorf("create bot api: %s", redactToken(err.Error(), token))
	}
	return &Client{bot: bot, chatID: parsedID, log: log}, nil
}

// endpointTransport redirects every outgoing request to a fixed scheme and
// host, leaving the path and query the library built untouched. It is the
// mechanism behind NewWithEndpoint; see that function's doc comment for why
// this indirection is needed instead of a constructor argument.
type endpointTransport struct {
	base   http.RoundTripper
	scheme string
	host   string
}

func (t *endpointTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme = t.scheme
	out.URL.Host = t.host
	out.Host = t.host
	return t.base.RoundTrip(out)
}

func (c *Client) SendGameImage(ctx context.Context, caption string, image []byte) error {
	if len(image) > maxPhotoBytes {
		return fmt.Errorf("photo is %d bytes, Telegram allows at most %d", len(image), maxPhotoBytes)
	}

	reader := bytes.NewReader(image)
	photo := tgbotapi.NewPhotoUpload(c.chatID, tgbotapi.FileReader{
		Name:   "game-recap.png",
		Reader: reader,
		Size:   int64(len(image)),
	})
	photo.Caption = caption

	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := c.bot.Send(photo); err != nil {
		return fmt.Errorf("send telegram photo: %s", redactToken(err.Error(), c.bot.Token))
	}
	c.log.Info().Str("caption", caption).Msg("game image sent")
	return nil
}

func redactToken(message, token string) string {
	if token == "" {
		return message
	}
	return strings.ReplaceAll(message, token, "<redacted>")
}
