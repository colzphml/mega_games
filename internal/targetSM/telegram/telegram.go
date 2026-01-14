package telegram

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/colzphml/mega_games/internal/model"
	"github.com/colzphml/mega_games/pkg/config"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rs/zerolog"
)

// log is the package-wide logger initialized to write to stdout with the package name as a contextual tag.
var log = zerolog.New(os.Stdout).With().Str("package", "telegram").Timestamp().Logger()

// Client represents a client for sending messages to Telegram.
type Client struct {
	Bot          *tgbotapi.BotAPI           // Bot instance for sending messages.
	ChatCommonId string                     // ID of the common chat to which messages are sent.
	ChatNewsId   string                     // ID of the news chat for announcements.
	Directory    string                     // Directory for storing files, not used currently.
	UrlPart      string                     // Part of the URL for constructing message contents, not used currently.
	TargetChan   <-chan model.TargetMessage // Channel for receiving messages to be sent.
}

// NewClient creates and initializes a new Telegram client with the provided configuration and message channel.
func NewClient(ctx context.Context, cfg *config.Config, targetChan <-chan model.TargetMessage) (*Client, error) {
	bot, err := tgbotapi.NewBotAPI(cfg.App.Target.Token)
	if err != nil {
		log.Error().Err(err).Msg("failed to create Telegram bot")
		return nil, err
	}

	bot.Debug = true
	log.Info().Msgf("authorized on account %s", bot.Self.UserName)

	return &Client{
		Bot:          bot,
		ChatCommonId: cfg.App.Target.CommonChannelID,
		ChatNewsId:   cfg.App.Target.NewsChannelID,
		Directory:    cfg.App.FileStoragePath,
		UrlPart:      cfg.App.GamesUrl,
		TargetChan:   targetChan,
	}, nil
}

// Close stops the Telegram bot's update receiving process.
func (c *Client) Close() {
	log.Info().Msg("closing telegram client")
	c.Bot.StopReceivingUpdates()
}

// ProceedSourceMessages listens for messages from the TargetChan and processes them according to their type.
func (c *Client) ProceedSourceMessages(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("context done, stopping source messages processing")
			return
		case message := <-c.TargetChan:
			switch message.Action {
			case "newWeek":
				if err := c.sendNewsMessage(message.Value); err != nil {
					log.Error().Err(err).Msg("failed to send news message")
				}
			case "game":
				if err := c.sendImage(message); err != nil {
					log.Error().Err(err).Msg("failed to send image")
				}
			case "schedule":
				log.Info().Msgf("schedule message received: %s", message.Value)
			default:
				log.Error().Msg("unknown message type received")
			}
		}
	}
}

// sendNewsMessage sends a news message to the news chat.
func (c *Client) sendNewsMessage(message string) error {
	chatNewsId, err := strconv.ParseInt(c.ChatNewsId, 10, 64)
	if err != nil {
		log.Error().Err(err).Msg("failed to convert chatNewsId to int")
		return err
	}

	moscowLoc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		log.Error().Err(err).Msg("failed to load Moscow timezone")
		return err
	}
	deadline := time.Now().In(moscowLoc).Add(time.Hour * 40).Format("02 Jan 2006 15:04")

	template := fmt.Sprintf("%s\n\n_❗️Пожалуйста, договоритесь прямо сейчас о матче во избежание затяжек шага.\n\nДо %s просьба указать анонс матча реплаем к этому посту_", message, deadline)

	msg := tgbotapi.NewMessage(chatNewsId, template)
	msg.DisableWebPagePreview = true
	msg.ParseMode = "Markdown"
	if _, err := c.Bot.Send(msg); err != nil {
		log.Error().Err(err).Msg("failed to send news message")
		return err
	}

	log.Info().Str("message", message).Msg("news message sent")
	return nil
}

// sendImage sends an image message to the common chat.
func (c *Client) sendImage(message model.TargetMessage) error {
	chatCommonId, err := strconv.ParseInt(c.ChatCommonId, 10, 64)
	if err != nil {
		log.Error().Err(err).Msg("failed to convert chatCommonId to int")
		return err
	}

	byteReader := bytes.NewReader(message.Image)
	photo := tgbotapi.NewPhotoUpload(chatCommonId, tgbotapi.FileReader{
		Name:   "game-recap.jpeg",
		Reader: byteReader,
		Size:   int64(len(message.Image)),
	})
	photo.Caption = message.Value

	// if _, err := c.Bot.Send(photo); err != nil {
	// 	log.Error().Err(err).Msg("failed to send image")
	// 	return err
	// }

	log.Debug().Str("url", message.Value).Msg("image sent")
	return nil
}
