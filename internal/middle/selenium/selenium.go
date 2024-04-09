package selenium

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/colzphml/mega_games/internal/model"
	"github.com/colzphml/mega_games/pkg/config"
	"github.com/rs/zerolog"
	"github.com/tebeka/selenium"
)

var log = zerolog.New(os.Stdout).With().Str("package", "selenium").Timestamp().Logger()

type Client struct {
	MiddleUrl       string
	FileStoragePath string
	MessageChan     <-chan model.DiscordGame
	TargetChan      chan<- model.TargetMessage
}

func NewClient(ctx context.Context, cfg *config.Config, messageChan <-chan model.DiscordGame, targetChan chan<- model.TargetMessage) (*Client, error) {
	return &Client{
		MiddleUrl:       cfg.App.Middle.MiddleUrl,
		FileStoragePath: cfg.App.FileStoragePath,
		MessageChan:     messageChan,
		TargetChan:      targetChan,
	}, nil
}

func (c *Client) Close() error {
	log.Info().Msg("closing selenium client")
	return nil
}

func (c *Client) ReadMessages(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("context done, closing selenium client")
			return
		case message := <-c.MessageChan:
			if err := c.proceedUrl(ctx, message.GameUrl); err != nil {
				log.Error().Err(err).Str("url", message.GameUrl).Msg("error proceeding URL")
			}
		}
	}
}

func (c *Client) proceedUrl(ctx context.Context, url string) error {
	caps := selenium.Capabilities{"browserName": "chrome"}
	wd, err := selenium.NewRemote(caps, c.MiddleUrl)
	if err != nil {
		return fmt.Errorf("error creating selenium session: %w", err)
	}
	defer wd.Quit()
	if err := wd.MaximizeWindow(""); err != nil {
		return fmt.Errorf("error maximizing window: %w", err)
	}
	if err := wd.Get(url); err != nil {
		return fmt.Errorf("error navigating to page: %w", err)
	}
	wd.SetImplicitWaitTimeout(2 * time.Second)
	if err := interactWithPage(wd); err != nil {
		return fmt.Errorf("error interacting with page: %w", err)
	}
	gamenumber := extractGameNumber(url)

	newPath, err := waitForFileAndRename(ctx, c.FileStoragePath, "game-recap.jpeg", gamenumber+".jpeg")
	if err != nil {
		return fmt.Errorf("error waiting for file and renaming: %w", err)
	}

	image, err := os.ReadFile(newPath)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}
	c.TargetChan <- model.TargetMessage{
		Action: "game",
		Value:  url,
		Image:  image,
	}

	return os.Remove(newPath)
}

func interactWithPage(wd selenium.WebDriver) error {
	// Find and click the RECAP button using XPath to locate by text
	recapBtn, err := wd.FindElement(selenium.ByXPATH, "//div[@id='q-app']/div/div/div/div[2]/main/div[2]/div[3]/div/div[3]/div[2]/div")
	if err != nil {
		log.Error().Err(err).Msg("error finding RECAP button")
		return err
	}
	if err := recapBtn.Click(); err != nil {
		log.Error().Err(err).Msg("error clicking RECAP button")
		return err
	}

	// Find and click the DOWNLOAD button using XPath to locate by class and text
	downloadBtn, err := wd.FindElement(selenium.ByXPATH, "//div[@id='q-app']/div/div/div/div[2]/main/div[2]/div[4]/div/div/div/div[4]/button/span[2]/i")
	if err != nil {
		log.Error().Err(err).Msg("error finding DOWNLOAD button")
		return err
	}
	if err := downloadBtn.Click(); err != nil {
		log.Error().Err(err).Msg("error clicking DOWNLOAD button")
		return err
	}
	return nil
}

func waitForFileAndRename(ctx context.Context, targetDir, oldFileName, newFileName string) (string, error) {
	targetPath := filepath.Join(targetDir, oldFileName)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			if _, err := os.Stat(targetPath); err == nil {
				newPath := filepath.Join(targetDir, newFileName)
				return newPath, os.Rename(targetPath, newPath)
			} else if !os.IsNotExist(err) {
				return "", fmt.Errorf("error while waiting for file: %v", err)
			}
		}
	}
}

func extractGameNumber(url string) string {
	parts := strings.Split(url, "/")
	return parts[len(parts)-1]
}
