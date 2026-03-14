package telegram

import (
	"bytes"
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

	// Telegram photo uploads intermittently fail with HTTP/2 stream errors on Pi.
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

func (c *Client) SendGameImage(caption string, image []byte) error {
	reader := bytes.NewReader(image)
	photo := tgbotapi.NewPhotoUpload(c.chatID, tgbotapi.FileReader{
		Name:   "game-recap.png",
		Reader: reader,
		Size:   int64(len(image)),
	})
	photo.Caption = caption
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
