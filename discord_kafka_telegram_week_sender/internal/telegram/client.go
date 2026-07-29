package telegram

import (
	"context"
	"fmt"
	"net"
	"net/http"
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

func New(token, chatID string, log zerolog.Logger) (*Client, error) {
	parsedID, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse chat id: %w", err)
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
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("create bot api: %s", redactToken(err.Error(), token))
	}
	return &Client{bot: bot, chatID: parsedID, log: log}, nil
}

func (c *Client) SendWeekMessage(ctx context.Context, text string) error {
	msg := tgbotapi.NewMessage(c.chatID, text)
	msg.DisableWebPagePreview = true
	msg.ParseMode = "MarkdownV2"

	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := c.bot.Send(msg); err != nil {
		return fmt.Errorf("send telegram message: %s", redactToken(err.Error(), c.bot.Token))
	}
	c.log.Info().Msg("week message sent")
	return nil
}

func redactToken(message, token string) string {
	if token == "" {
		return message
	}
	return strings.ReplaceAll(message, token, "<redacted>")
}
