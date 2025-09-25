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

// log is the package-level logger configured for structured logging.
var log = zerolog.New(os.Stdout).With().Str("package", "selenium").Timestamp().Logger()

// Client represents a Selenium client that interacts with web pages and processes messages.
type Client struct {
	MiddleUrl       string                     // URL to the Selenium WebDriver server.
	FileStoragePath string                     // Path to store downloaded files.
	MessageChan     <-chan model.DiscordGame   // Channel for incoming Discord game messages.
	TargetChan      chan<- model.TargetMessage // Channel to send processed target messages.
}

// NewClient initializes a new Selenium client with the provided configuration and channels.
func NewClient(ctx context.Context, cfg *config.Config, messageChan <-chan model.DiscordGame, targetChan chan<- model.TargetMessage) (*Client, error) {
	return &Client{
		MiddleUrl:       cfg.App.Middle.MiddleUrl,
		FileStoragePath: cfg.App.FileStoragePath,
		MessageChan:     messageChan,
		TargetChan:      targetChan,
	}, nil
}

// Close performs any necessary cleanup before the client is disposed of.
func (c *Client) Close() error {
	log.Info().Msg("closing selenium client")
	return nil
}

// ReadMessages listens for messages on the MessageChan and processes them accordingly.
func (c *Client) ReadMessages(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("context done, stopping message reading")
			return
		case message := <-c.MessageChan:
			if err := c.proceedUrl(ctx, message.GameUrl); err != nil {
				log.Error().Err(err).Str("url", message.GameUrl).Msg("failed to process URL")
			}
		}
	}
}

// proceedUrl handles the processing of a specific game URL.
func (c *Client) proceedUrl(ctx context.Context, url string) error {
	caps := selenium.Capabilities{"browserName": "chrome"}
	wd, err := selenium.NewRemote(caps, c.MiddleUrl)
	if err != nil {
		return fmt.Errorf("creating selenium session failed: %w", err)
	}
	defer wd.Quit()

	if err := wd.MaximizeWindow(""); err != nil {
		return fmt.Errorf("maximizing window failed: %w", err)
	}
	if err := wd.Get(url); err != nil {
		return fmt.Errorf("navigating to page failed: %w", err)
	}

	wd.SetImplicitWaitTimeout(2 * time.Second)
	if err := interactWithPage(wd); err != nil {
		return fmt.Errorf("interacting with page failed: %w", err)
	}

	gamenumber := extractGameNumber(url)

	newPath, err := waitForFileAndRename(ctx, c.FileStoragePath, "game-recap.jpeg", gamenumber+".jpeg")
	if err != nil {
		return fmt.Errorf("waiting for file and renaming failed: %w", err)
	}

	image, err := os.ReadFile(newPath)
	if err != nil {
		return fmt.Errorf("reading file failed: %w", err)
	}

	c.TargetChan <- model.TargetMessage{
		Action: "game",
		Value:  url,
		Image:  image,
	}

	return os.Remove(newPath)
}

// interactWithPage взаимодействует с веб-страницей, например, нажимает кнопки.
func interactWithPage(wd selenium.WebDriver) error {
	log.Info().Msg("Начинаем взаимодействие со страницей")

	// 1. Попытаться найти и нажать кнопку принятия cookies (ищем любой button с текстом 'Consent')
	if consentBtns, _ := wd.FindElements(selenium.ByXPATH, "//button[contains(., 'Consent')]"); len(consentBtns) > 0 {
		log.Info().Msg("Нашли кнопку Consent, нажимаем")
		if err := consentBtns[0].Click(); err != nil {
			log.Error().Err(err).Msg("Ошибка при нажатии на кнопку Consent")
		} else {
			// Ждать не нужно фиксированное время, можно просто подождать один кадр JS
			time.Sleep(500 * time.Millisecond)
		}
	} else {
		log.Info().Msg("Кнопка Consent не найдена, возможно, cookies уже приняты")
	}

	// 2. Нажимаем вкладку Recap (поиск по тексту метки)
	recapXPath := "//div[contains(@class,'q-tab')][.//div[@class='q-tab__label' and normalize-space()='Recap']]"
	recapTab, err := wd.FindElement(selenium.ByXPATH, recapXPath)
	if err != nil {
		log.Error().Err(err).Msg("Не удалось найти вкладку Recap")
		return fmt.Errorf("вкладка Recap не найдена: %w", err)
	}
	if err := recapTab.Click(); err != nil {
		log.Error().Err(err).Msg("Ошибка при клике по вкладке Recap")
		return fmt.Errorf("ошибка при клике по вкладке Recap: %w", err)
	}
	log.Info().Msg("Вкладка Recap успешно выбрана")

	// 3. Ждём появления кнопки DOWNLOAD без фиксированного ожидания.
	xpaths := []string{
		"//span[normalize-space()='DOWNLOAD']/ancestor::button[1]",                                                                                 // span внутри button
		"//button[.//span[contains(translate(., 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'download')]]",                        // button c текстом download
		"//*[@role='button'][contains(translate(., 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'download')]",                      // любой элемент с role=button
		"//div[contains(@class,'q-btn')][.//span[contains(translate(., 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'download')]]", // div с классом q-btn
	}
	var downloadBtn selenium.WebElement
	timeout := 15 * time.Second
	interval := 500 * time.Millisecond
	start := time.Now()

	for time.Since(start) < timeout && downloadBtn == nil {
		for _, xp := range xpaths {
			elem, findErr := wd.FindElement(selenium.ByXPATH, xp)
			if findErr == nil && elem != nil {
				downloadBtn = elem
				log.Info().Msgf("Нашли кнопку DOWNLOAD по XPath: %s", xp)
				break
			} else {
				log.Debug().Msgf("XPath не сработал: %s", xp)
			}
		}
		if downloadBtn == nil {
			time.Sleep(interval)
		}
	}
	if downloadBtn == nil {
		log.Error().Msg("Не удалось найти кнопку DOWNLOAD в течение 15 секунд")
		return fmt.Errorf("кнопка DOWNLOAD не найдена")
	}

	// 4. Кликаем по найденному элементу
	if err := downloadBtn.Click(); err != nil {
		log.Error().Err(err).Msg("Ошибка при нажатии кнопки DOWNLOAD")
		return fmt.Errorf("ошибка при клике по кнопке DOWNLOAD: %w", err)
	}
	log.Info().Msg("Кнопка DOWNLOAD успешно нажата")
	return nil
}

// waitForFileAndRename waits for a specific file to appear and renames it.
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

// extractGameNumber extracts the game number from a URL.
func extractGameNumber(url string) string {
	parts := strings.Split(url, "/")
	return parts[len(parts)-1]
}
