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

	log.Info().Str("path", newPath).Msg("file found")
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

func makeScreenshot(wd selenium.WebDriver, filename string) {
	// Сделать скриншот
	img, err := wd.Screenshot()
	if err != nil {
		log.Info().Msg("screenshot " + filename + " failed")
	}
	// Сохранить скриншот в файл
	err = os.WriteFile("/mega/screens/"+filename+".png", img, 0644)
	if err != nil {
		log.Info().Msg("screenshot " + filename + " save failed")
	}
	log.Info().Msg("screenshot " + filename + " saved")
}

// waitUntil ждёт, пока условие cond вернёт true, либо пока не истечёт timeout.
// Проверки выполняются каждые interval.
func waitUntil(cond func() bool, timeout, interval time.Duration) bool {
	start := time.Now()
	for time.Since(start) < timeout {
		if cond() {
			return true
		}
		time.Sleep(interval)
	}
	return false
}

func waitPage(wd selenium.WebDriver) {
	for {
		state, err := wd.ExecuteScript("return document.readyState", nil)
		if err != nil {
			log.Fatal()
		}
		if state == "complete" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// interactWithPage взаимодействует с веб-страницей, например, нажимает кнопки.
func interactWithPage(wd selenium.WebDriver) error {
	log.Info().Msg("Начинаем взаимодействие со страницей")

	// Принимаем cookies, если требуется
	if consentBtns, _ := wd.FindElements(selenium.ByXPATH, "//button[contains(., 'Consent')]"); len(consentBtns) > 0 {
		if err := consentBtns[0].Click(); err != nil {
			log.Warn().Err(err).Msg("Не удалось нажать кнопку Consent")
		}
	}

	time.Sleep(2 * time.Second)

	// Ожидаем появления вкладки Recap с тайм‑аутом 10 секунд
	var recapTab selenium.WebElement
	recapTabXPath := "//*[contains(@class,'q-tab') and @role='tab'][.//div[contains(@class,'q-tab__label') and translate(normalize-space(.), 'ABCDEFGHIJKLMNOPQRSTUVWXYZ','abcdefghijklmnopqrstuvwxyz')='recap']]"
	foundRecap := waitUntil(func() bool {
		elem, err := wd.FindElement(selenium.ByXPATH, recapTabXPath)
		if err == nil && elem != nil {
			recapTab = elem
			return true
		}
		return false
	}, 10*time.Second, 500*time.Millisecond)
	if !foundRecap {
		return fmt.Errorf("вкладка Recap не найдена")
	}

	// Переключаемся на вкладку Recap
	if err := recapTab.Click(); err != nil {
		return fmt.Errorf("не удалось кликнуть по вкладке Recap: %w", err)
	}

	// // Прокручиваем страницу, чтобы загрузился контент Recap
	// _, _ = wd.ExecuteScript("window.scrollTo(0, document.body.scrollHeight);", nil)

	// Ожидаем появления кнопки Download (только <button> с текстом DOWNLOAD)
	var downloadBtn selenium.WebElement
	foundDownload := waitUntil(func() bool {
		elem, err := wd.FindElement(selenium.ByXPATH, "//button[contains(translate(., 'abcdefghijklmnopqrstuvwxyz','ABCDEFGHIJKLMNOPQRSTUVWXYZ'), 'DOWNLOAD')]")
		if err == nil && elem != nil {
			downloadBtn = elem
			return true
		}
		return false
	}, 15*time.Second, 500*time.Millisecond)
	if !foundDownload {
		return fmt.Errorf("кнопка Download не найдена")
	}

	time.Sleep(2 * time.Second)
	// Скроллим к кнопке, чтобы она не перекрывалась тулбаром
	_, _ = wd.ExecuteScript(
		"arguments[0].scrollIntoView({block: 'center', inline: 'nearest'})",
		[]interface{}{downloadBtn},
	)

	// Пытаемся нажать на кнопку Download
	if err := downloadBtn.Click(); err != nil {
		return fmt.Errorf("ошибка при клике по Download: %w", err)
	}

	log.Info().Msg("Кнопка Download успешно нажата")
	return nil
}

// waitForFileAndRename waits for a specific file to appear and renames it.
func waitForFileAndRename(ctx context.Context, targetDir, oldFileName, newFileName string) (string, error) {
	log.Info().Msg("Старт ожидания файла")
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
