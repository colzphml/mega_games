package telegram

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/colzphml/mega_games/internal/model"
	"github.com/colzphml/mega_games/pkg/config"
	"github.com/rs/zerolog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

var log = zerolog.New(os.Stdout).With().Str("package", "telegram").Timestamp().Logger()

// const fileExtension = ".jpeg"
// const processedFilePrefix = "game-recap"

type Client struct {
	Bot          *tgbotapi.BotAPI
	ChatCommonId string
	ChatNewsId   string
	Directory    string
	UrlPart      string
	TargetChan   <-chan model.TargetMessage
}

func NewClient(ctx context.Context, cfg *config.Config, targetChan <-chan model.TargetMessage) (*Client, error) {
	bot, err := tgbotapi.NewBotAPI(cfg.App.Target.Token)
	if err != nil {
		log.Error().Err(err).Msg("failed to create Telegram bot")
		return nil, err
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)
	return &Client{
		Bot:          bot,
		ChatCommonId: cfg.App.Target.CommonChannelID,
		ChatNewsId:   cfg.App.Target.NewsChannelID,
		Directory:    cfg.App.FileStoragePath,
		UrlPart:      cfg.App.GamesUrl,
		TargetChan:   targetChan,
	}, nil
}

func (c *Client) Close() {
	log.Info().Msg("closing telegram client")
	c.Bot.StopReceivingUpdates()
}

// func (c *Client) deleteExistingJPEGs() {
// 	files, err := os.ReadDir(c.Directory)
// 	if err != nil {
// 		log.Error().Err(err).Msg("error reading directory")
// 		return
// 	}

// 	for _, file := range files {
// 		if filepath.Ext(file.Name()) == fileExtension {
// 			if err := os.Remove(filepath.Join(c.Directory, file.Name())); err != nil {
// 				log.Error().Err(err).Str("file", file.Name()).Msg("error deleting file")
// 			} else {
// 				log.Info().Str("file", file.Name()).Msg("deleted")
// 			}
// 		}
// 	}
// }

// func (c *Client) processFiles() {
// 	files, err := os.ReadDir(c.Directory)
// 	if err != nil {
// 		log.Error().Err(err).Msg("error reading directory")
// 		return
// 	}

// 	for _, file := range files {
// 		if filepath.Ext(file.Name()) == fileExtension && !strings.Contains(file.Name(), processedFilePrefix) {
// 			err := c.sendImage(file.Name())
// 			if err != nil {
// 				log.Error().Err(err).Str("file", file.Name()).Msg("error sending image")
// 			}
// 			log.Info().Str("file", file.Name()).Str("file", file.Name()).Msg("proceeding with file")
// 			// After processing, remove the file
// 			if err := os.Remove(filepath.Join(c.Directory, file.Name())); err != nil {
// 				log.Error().Err(err).Str("file", file.Name()).Msg("error deleting file")
// 			}
// 		} else {
// 			continue
// 		}
// 	}
// }

// func (c *Client) ProceedFiles(ctx context.Context, wg *sync.WaitGroup) {
// 	defer wg.Done()

// 	c.deleteExistingJPEGs()

// 	ticker := time.NewTicker(100 * time.Millisecond)
// 	defer ticker.Stop()

// 	for {
// 		select {
// 		case <-ctx.Done():
// 			log.Info().Msg("context done, stopping file processing")
// 			return
// 		case <-ticker.C:
// 			c.processFiles()
// 		}
// 	}
// }

func (c *Client) ProceedSourceMessages(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("context done, stopping source messages processing")
			return
		case message := <-c.TargetChan:
			switch message.Action {
			case "news":
				err := c.sendNewsMessage(message.Value)
				if err != nil {
					log.Error().Err(err).Msg("failed to send news message")
				}
			case "game":
				err := c.sendImage(message)
				if err != nil {
					log.Error().Err(err).Msg("failed to send image")
				}
			case "schedule":
				log.Info().Msg("schedule message received" + message.Value)
			default:
				log.Error().Msg("unknown message type")
			}
		}
	}
}

func (c *Client) sendNewsMessage(message string) error {
	chatNewsId, err := strconv.ParseInt(c.ChatNewsId, 10, 64)
	if err != nil {
		log.Error().Err(err).Msg("failed to convert chatNewsId to int")
		return err
	}
	template := fmt.Sprintf("%s\n\n_❗️Пожалуйста, договоритесь прямо сейчас о матче во избежание затяжек шага.\n\nАнонсы игр указывайте реплаем к этому посту_", message)

	msg := tgbotapi.NewMessage(chatNewsId, template)
	msg.DisableWebPagePreview = true
	msg.ParseMode = "Markdown"
	if _, err := c.Bot.Send(msg); err != nil {
		log.Error().Err(err).Msg("failed to send message")
		return err
	} else {
		log.Info().Str("message", message).Msg("message sent")
	}
	return nil
}

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

	if _, err := c.Bot.Send(photo); err != nil {
		log.Error().Err(err).Msg("failed to send image")
		return err
	} else {
		log.Debug().Str("url", message.Value).Msg("image sent")
	}
	return nil
}
