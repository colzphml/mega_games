package telegram

import (
	"fmt"
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

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = false
	bot, err := tgbotapi.NewBotAPIWithClient(token, &http.Client{
		Timeout:   2 * time.Minute,
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("create bot api: %w", err)
	}
	return &Client{bot: bot, chatID: parsedID, log: log}, nil
}

func (c *Client) SendWeekMessage(text string) error {
	msg := tgbotapi.NewMessage(c.chatID, text)
	msg.DisableWebPagePreview = true
	msg.ParseMode = "Markdown"
	if _, err := c.bot.Send(msg); err != nil {
		return fmt.Errorf("send telegram message: %s", redactToken(err.Error(), c.bot.Token))
	}
	c.log.Info().Str("message", text).Msg("week message sent")
	return nil
}

func redactToken(message, token string) string {
	if token == "" {
		return message
	}
	return strings.ReplaceAll(message, token, "<redacted>")
}
