package selenium

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/colzphml/mega_games/pkg/config"
	"github.com/rs/zerolog"
	"github.com/tebeka/selenium"
)

var log = zerolog.New(os.Stdout).With().Str("package", "selenium").Timestamp().Logger()

type Client struct {
	MiddleUrl       string
	FileStoragePath string
}

func NewClient(ctx context.Context, cfg *config.Config) (*Client, error) {
	return &Client{
		MiddleUrl:       cfg.App.Middle.MiddleUrl,
		FileStoragePath: cfg.App.FileStoragePath,
	}, nil
}

func (c *Client) Close() error {
	log.Info().Msg("closing selenium client")
	return nil
}

func (c *Client) ReadMessages(ctx context.Context, wg *sync.WaitGroup, messagesChan <-chan string) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("context done, closing selenium client")
			return
		case message := <-messagesChan:
			if err := c.proceedUrl(ctx, message); err != nil {
				log.Error().Err(err).Str("url", message).Msg("error proceeding URL")
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
	log.Info().Msg("COLZ:1")
	if err := wd.MaximizeWindow(""); err != nil {
		return fmt.Errorf("error maximizing window: %w", err)
	}
	log.Info().Msg("COLZ:2")
	if err := wd.Get(url); err != nil {
		return fmt.Errorf("error navigating to page: %w", err)
	}
	log.Info().Msg("COLZ:3")
	wd.SetImplicitWaitTimeout(2 * time.Second)
	log.Info().Msg("COLZ:4")
	if err := interactWithPage(wd); err != nil {
		return fmt.Errorf("error interacting with page: %w", err)
	}
	log.Info().Msg("COLZ:5")
	gamenumber := extractGameNumber(url)
	return waitForFileAndRename(ctx, c.FileStoragePath, "game-recap.jpeg", gamenumber+".jpeg")
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

func waitForFileAndRename(ctx context.Context, targetDir, oldFileName, newFileName string) error {
	targetPath := filepath.Join(targetDir, oldFileName)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := os.Stat(targetPath); err == nil {
				newPath := filepath.Join(targetDir, newFileName)
				return os.Rename(targetPath, newPath)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("error while waiting for file: %v", err)
			}
		}
	}
}

func extractGameNumber(url string) string {
	parts := strings.Split(url, "/")
	return parts[len(parts)-1]
}
