package telegram

import (
	"fmt"
	"strconv"

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
	bot, err := tgbotapi.NewBotAPI(token)
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
		return fmt.Errorf("send telegram message: %w", err)
	}
	c.log.Info().Str("message", text).Msg("week message sent")
	return nil
}
