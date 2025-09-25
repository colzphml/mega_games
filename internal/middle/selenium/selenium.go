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
	// Увеличиваем время ожидания для страницы
	wd.SetImplicitWaitTimeout(10 * time.Second)

	// Попытка найти и нажать кнопку принятия куки с альтернативными селекторами
	consentSelectors := []string{
		"//p[@class='fc-button-label' and text()='Consent']",
		"//button[contains(text(), 'Consent')]",
		"//button[contains(@class, 'fc-') and contains(text(), 'Consent')]",
		"//div[contains(@class, 'fc-consent')]//button[contains(text(), 'Consent')]",
		"//button[@id='fc-consent-button']",
	}

	for _, selector := range consentSelectors {
		consentBtn, err := wd.FindElement(selenium.ByXPATH, selector)
		if err == nil {
			if err := consentBtn.Click(); err != nil {
				log.Error().Err(err).Msg("Ошибка при нажатии кнопки Consent")
				continue
			}
			log.Info().Msg("Кнопка принятия куки успешно нажата")
			time.Sleep(2 * time.Second)
			break
		}
	}

	// Поиск и нажатие кнопки RECAP с несколькими вариантами селекторов
	recapSelectors := []string{
		"//button[contains(text(), 'RECAP')]",
		"//div[contains(text(), 'RECAP')]",
		"//span[contains(text(), 'RECAP')]",
		"//a[contains(text(), 'RECAP')]",
		"//div[@class='q-tab__label' and text()='RECAP']",
		"//div[contains(@class, 'q-tab') and contains(text(), 'RECAP')]",
		"//div[@role='tab' and contains(text(), 'RECAP')]",
	}

	var recapBtn selenium.WebElement
	var recapErr error

	for _, selector := range recapSelectors {
		recapBtn, recapErr = wd.FindElement(selenium.ByXPATH, selector)
		if recapErr == nil {
			break
		}
	}

	if recapErr != nil {
		log.Error().Err(recapErr).Msg("Ошибка при поиске кнопки RECAP")
		return recapErr
	}

	if err := recapBtn.Click(); err != nil {
		log.Error().Err(err).Msg("Ошибка при нажатии кнопки RECAP")
		return err
	}

	log.Info().Msg("Кнопка RECAP успешно нажата")
	time.Sleep(3 * time.Second)

	// Поиск и нажатие кнопки DOWNLOAD с множественными селекторами
	downloadSelectors := []string{
		"//button[contains(text(), 'DOWNLOAD')]",
		"//a[contains(text(), 'DOWNLOAD')]",
		"//span[contains(text(), 'DOWNLOAD')]",
		"//button//span[contains(text(), 'DOWNLOAD')]",
		"//div[@class='q-btn__content']//span[contains(text(), 'DOWNLOAD')]",
		"//button[contains(@class, 'q-btn')]//span[contains(text(), 'DOWNLOAD')]",
		"//div[contains(@class, 'q-btn') and contains(text(), 'DOWNLOAD')]",
		"//div[@role='button' and contains(text(), 'DOWNLOAD')]",
		"//button[.//text()[contains(., 'DOWNLOAD')]]",
		"//i[contains(@class, 'download')]",
	}

	var downloadBtn selenium.WebElement
	var downloadErr error

	for _, selector := range downloadSelectors {
		downloadBtn, downloadErr = wd.FindElement(selenium.ByXPATH, selector)
		if downloadErr == nil {
			log.Info().Str("selector", selector).Msg("Найдена кнопка DOWNLOAD")
			break
		}
	}

	if downloadErr != nil {
		log.Error().Err(downloadErr).Msg("Ошибка при поиске кнопки DOWNLOAD")
		return downloadErr
	}

	// Проверяем, что элемент видим и кликабелен
	if displayed, err := downloadBtn.IsDisplayed(); err != nil || !displayed {
		log.Error().Msg("Кнопка DOWNLOAD не видна")
		return fmt.Errorf("кнопка DOWNLOAD не видна")
	}

	if enabled, err := downloadBtn.IsEnabled(); err != nil || !enabled {
		log.Error().Msg("Кнопка DOWNLOAD не активна")
		return fmt.Errorf("кнопка DOWNLOAD не активна")
	}

	if err := downloadBtn.Click(); err != nil {
		log.Error().Err(err).Msg("Ошибка при нажатии кнопки DOWNLOAD")
		return err
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
