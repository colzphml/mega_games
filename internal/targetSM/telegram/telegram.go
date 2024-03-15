package telegram

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colzphml/mega_games/pkg/config"
	"github.com/rs/zerolog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

var log = zerolog.New(os.Stdout).With().Str("package", "telegram").Timestamp().Logger()

const fileExtension = ".jpeg"
const processedFilePrefix = "game-recap"

type Client struct {
	Bot       *tgbotapi.BotAPI
	ChatId    string
	Directory string
	UrlPart   string
}

func NewClient(ctx context.Context, cfg *config.Config) (*Client, error) {
	bot, err := tgbotapi.NewBotAPI(cfg.App.Target.Token)
	if err != nil {
		log.Error().Err(err).Msg("failed to create Telegram bot")
		return nil, err
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)
	return &Client{
		Bot:       bot,
		ChatId:    cfg.App.Target.ChannelID,
		Directory: cfg.App.FileStoragePath,
		UrlPart:   cfg.App.GamesUrl,
	}, nil
}

func (c *Client) Close() {
	log.Info().Msg("closing telegram client")
	//close bot connection
	c.Bot.StopReceivingUpdates()
}

func (c *Client) deleteExistingJPEGs() {
	files, err := os.ReadDir(c.Directory)
	if err != nil {
		log.Error().Err(err).Msg("error reading directory")
		return
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) == fileExtension {
			if err := os.Remove(filepath.Join(c.Directory, file.Name())); err != nil {
				log.Error().Err(err).Str("file", file.Name()).Msg("error deleting file")
			} else {
				log.Info().Str("file", file.Name()).Msg("deleted")
			}
		}
	}
}

func (c *Client) processFiles() {
	files, err := os.ReadDir(c.Directory)
	if err != nil {
		log.Error().Err(err).Msg("error reading directory")
		return
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) == fileExtension && !strings.Contains(file.Name(), processedFilePrefix) {
			err := c.sendImage(file.Name())
			if err != nil {
				log.Error().Err(err).Str("file", file.Name()).Msg("error sending image")
				continue
			}
			log.Info().Str("file", file.Name()).Str("file", file.Name()).Msg("proceeding with file")
			// After processing, remove the file
			if err := os.Remove(filepath.Join(c.Directory, file.Name())); err != nil {
				log.Error().Err(err).Str("file", file.Name()).Msg("error deleting file")
			}
		} else {
			continue
		}
	}
}

func (c *Client) ProceedFiles(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	c.deleteExistingJPEGs()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("context done, stopping file processing")
			return
		case <-ticker.C:
			c.processFiles()
		}
	}
}

func (c *Client) sendImage(fileName string) error {
	chatId, err := strconv.ParseInt(c.ChatId, 10, 64)
	if err != nil {
		log.Error().Err(err).Msg("failed to convert chatId to int")
		return err
	}

	filePath := filepath.Join(c.Directory, fileName)
	msg := tgbotapi.NewPhotoUpload(chatId, filePath)
	gameNumber := strings.TrimSuffix(fileName, fileExtension)
	fullURL := c.UrlPart + gameNumber
	msg.Caption = fullURL

	if _, err := c.Bot.Send(msg); err != nil {
		log.Error().Err(err).Msg("failed to send image")
		return err
	} else {
		log.Debug().Str("file", fileName).Msg("image sent")
	}
	return nil
}
