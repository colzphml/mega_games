package telegram

import (
	"bytes"
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

func (c *Client) SendGameImage(caption string, image []byte) error {
	reader := bytes.NewReader(image)
	photo := tgbotapi.NewPhotoUpload(c.chatID, tgbotapi.FileReader{
		Name:   "game-recap.png",
		Reader: reader,
		Size:   int64(len(image)),
	})
	photo.Caption = caption
	if _, err := c.bot.Send(photo); err != nil {
		return fmt.Errorf("send telegram photo: %w", err)
	}
	c.log.Info().Str("caption", caption).Msg("game image sent")
	return nil
}
