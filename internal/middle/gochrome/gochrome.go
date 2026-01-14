package gochrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/colzphml/mega_games/internal/model"
	"github.com/colzphml/mega_games/pkg/config"
	"github.com/rs/zerolog"
)

const (
	defaultTimeout    = 120 * time.Second // чуть увеличим, потому что иногда тяжелые страницы
	defaultAPITimeout = 15 * time.Second
	defaultAssetsWait = 45 * time.Second

	defaultViewportW  = 2600
	defaultViewportH  = 1500
	defaultHeadless   = true
	defaultSleepAfter = 0 * time.Second

	defaultScreenshotDir = "../screens"

	defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130 Safari/537.36"
)

var log = zerolog.New(os.Stdout).With().Str("package", "gochrome").Timestamp().Logger()

// Client renders recap images from NeonSportz web UI via headless Chrome (chromedp).
type Client struct {
	MessageChan <-chan model.DiscordGame
	TargetChan  chan<- model.TargetMessage
}

func NewClient(ctx context.Context, cfg *config.Config, messageChan <-chan model.DiscordGame, targetChan chan<- model.TargetMessage) (*Client, error) {
	return &Client{MessageChan: messageChan, TargetChan: targetChan}, nil
}

func (c *Client) Close() error {
	log.Info().Msg("closing gochrome client")
	return nil
}

func (c *Client) ReadMessages(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("context done, stopping message reading")
			return
		case message := <-c.MessageChan:
			if err := c.proceedGame(ctx, message); err != nil {
				log.Error().Err(err).Str("url", message.GameUrl).Msg("failed to process game")
			}
		}
	}
}

func (c *Client) proceedGame(ctx context.Context, message model.DiscordGame) error {
	gameURL, league, gameID, err := buildGameURL(message.GameUrl, "", "")
	if err != nil {
		return err
	}

	apiURL := fmt.Sprintf("https://neonsportz.com/api/leagues/%s/games/%s/recap/", url.PathEscape(league), url.PathEscape(gameID))

	// 1) API precheck (fast fail)
	recap, err := fetchRecapJSON(apiURL, defaultUserAgent, defaultAPITimeout)
	if err != nil {
		return fmt.Errorf("api precheck failed: %w", err)
	}
	if recap.Game.PK == 0 {
		return fmt.Errorf("api precheck failed: recap.game.pk is empty (unexpected response)")
	}
	if recap.Game.Status <= 1 {
		return fmt.Errorf("recap is not available: game.status=%d (usually means not completed yet)", recap.Game.Status)
	}

	// 2) Headless Chrome screenshot
	pngBytes, err := screenshotRecap(ctx, gameURL, defaultTimeout, defaultHeadless, defaultSleepAfter, defaultAssetsWait, defaultViewportW, defaultViewportH)
	if err != nil {
		return fmt.Errorf("screenshot failed: %w", err)
	}
	if len(pngBytes) == 0 {
		return fmt.Errorf("empty screenshot bytes")
	}

	if saveErr := saveScreenshot(defaultScreenshotDir, gameID, pngBytes); saveErr != nil {
		log.Error().Err(saveErr).Str("dir", defaultScreenshotDir).Msg("failed to save screenshot")
	}

	c.TargetChan <- model.TargetMessage{
		Action: "game",
		Value:  message.GameUrl,
		Image:  pngBytes,
	}

	return nil
}

func saveScreenshot(dir, gameID string, data []byte) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("empty screenshot dir")
	}
	if strings.TrimSpace(gameID) == "" {
		return fmt.Errorf("empty game id")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create screenshot dir: %w", err)
	}
	path := filepath.Join(dir, gameID+".png")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write screenshot: %w", err)
	}
	return nil
}

// -------------------------
// API precheck
// -------------------------

type RecapResponse struct {
	Game Game `json:"game"`
}

type Game struct {
	PK        int  `json:"pk"`
	HomeScore int  `json:"homeScore"`
	AwayScore int  `json:"awayScore"`
	Status    int  `json:"status"`
	HomeTeam  Team `json:"homeTeam"`
	AwayTeam  Team `json:"awayTeam"`
}

type Team struct {
	DisplayName  string `json:"displayName"`
	AbbrName     string `json:"abbrName"`
	CityName     string `json:"cityName"`
	LogoID       int    `json:"logoId"`
	PrimaryColor string `json:"primaryColor"`
}

func fetchRecapJSON(apiURL, userAgent string, to time.Duration) (*RecapResponse, error) {
	client := &http.Client{Timeout: to}

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 404:
		return nil, fmt.Errorf("recap endpoint returned 404 (wrong league/game, or recap not published): %s", apiURL)
	case 429:
		return nil, fmt.Errorf("rate limited (429) by neonsportz; retry later: %s", apiURL)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("unexpected status %d from API; body=%q", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	dec := json.NewDecoder(resp.Body)
	var rr RecapResponse
	if err := dec.Decode(&rr); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}
	return &rr, nil
}

// -------------------------
// Headless Chrome screenshot
// -------------------------

func screenshotRecap(
	parentCtx context.Context,
	gameURL string,
	overallTimeout time.Duration,
	headless bool,
	sleepAfter time.Duration,
	assetsWait time.Duration,
	viewportW, viewportH int,
) ([]byte, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("headless", headless),
		chromedp.Flag("disable-dev-shm-usage", true),

		// чуть меньше шансов, что сайт будет вести себя иначе
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(parentCtx, allocOpts...)
	defer cancel()

	ctx, cancel2 := context.WithTimeout(allocCtx, overallTimeout)
	defer cancel2()

	ctx, cancel3 := chromedp.NewContext(ctx)
	defer cancel3()

	var pngBytes []byte
	var stage string
	recapSelector := "#recap-wrapper"

	setStage := func(name string) chromedp.ActionFunc {
		return func(ctx context.Context) error {
			stage = name
			log.Debug().Str("stage", stage).Msg("gochrome step")
			return nil
		}
	}

	tasks := chromedp.Tasks{
		setStage("emulation"),
		emulation.SetDeviceMetricsOverride(int64(viewportW), int64(viewportH), 1.0, false),
		emulation.SetUserAgentOverride(defaultUserAgent),

		// ВКЛЮЧАЕМ network + блокировку CMP (без fetch, без дедлоков)
		setStage("network"),
		network.Enable(),
		network.SetBlockedURLs(cmpBlockedURLPatterns()),

		setStage("navigate"),
		chromedp.Navigate(gameURL),
		chromedp.WaitReady("body", chromedp.ByQuery),

		// НЕ принимаем cookies — просто убираем оверлей.
		setStage("strip consent overlay"),
		stripConsentOverlay(),

		setStage("recap tab"),
		waitAndClickRecapTab(),

		setStage("recap wrapper"),
		waitRecapWrapperRobust(assetsWait, &recapSelector),
		chromedp.ScrollIntoView(recapSelector, chromedp.ByQuery),

		setStage("assets"),
		waitRecapAssetsLoadedSync(assetsWait, &recapSelector),

		// CMP может “догрузиться” и показаться позже — убираем ещё раз перед скрином.
		setStage("pre-screenshot cleanup"),
		stripConsentOverlay(),

		setStage("sleep after"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if sleepAfter > 0 {
				time.Sleep(sleepAfter)
			}
			return nil
		}),

		setStage("screenshot"),
		chromedp.Screenshot(recapSelector, &pngBytes, chromedp.NodeVisible, chromedp.ByQuery),
	}

	if err := chromedp.Run(ctx, tasks); err != nil {
		return nil, fmt.Errorf("stage %s: %w", stage, err)
	}
	return pngBytes, nil
}

// -------------------------
// CMP blocking (safe, no fetch intercept)
// -------------------------

func cmpBlockedURLPatterns() []string {
	// Это паттерны CDP network.SetBlockedURLS (поддерживает * wildcard).
	// Стараемся быть аккуратными: блокируем типичные CMP/consent вендоры.
	return []string{
		"*://*/*quantcast*",
		"*://*/*qc-cmp*",
		"*://*/*cmp2*",

		"*://*/*didomi*",
		"*://*/*onetrust*",
		"*://*/*cookiebot*",
		"*://*/*trustarc*",
		"*://*/*truste*",
		"*://*/*iubenda*",
		"*://*/*sourcepoint*",
		"*://*/*consentmanager*",
		"*://*/*privacy-manager*",
		"*://*/*privacy-mgmt*",

		// generic consent/tcf (осторожно — может задеть что-то лишнее, но обычно ок)
		"*://*/*iab*consent*",
		"*://*/*tcf*",
		"*://*/*gdpr*consent*",
	}
}

// -------------------------
// UI helpers
// -------------------------

// stripConsentOverlay removes/hides cookie/CMP overlays WITHOUT accepting consent.
// It hides overlay-like nodes (fixed + big + high z-index) and restores body/html scrolling.
func stripConsentOverlay() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		js := `
(() => {
  try {
    const removed = [];

    const hide = (el) => {
      if (!el) return;
      el.style.setProperty("display", "none", "important");
      el.style.setProperty("visibility", "hidden", "important");
      el.style.setProperty("opacity", "0", "important");
      el.style.setProperty("pointer-events", "none", "important");
    };

    const looksLikeOverlay = (n) => {
      const s = getComputedStyle(n);
      const r = n.getBoundingClientRect();
      const z = parseFloat(s.zIndex || "0");
      return (s.position === "fixed" || s.position === "sticky") &&
             r.width > 200 && r.height > 150 &&
             z >= 100;
    };

    const selectors = [
      "#qc-cmp2-container", ".qc-cmp2-container", ".qc-cmp2-main",
      "#didomi-host", "#didomi-popup", ".didomi-popup",
      "#onetrust-banner-sdk", "#ot-sdk-btn-floating", ".ot-sdk-container",
      "#cookie-banner", ".cookie-banner", ".cookie-consent",
      "[id*='cmp']", "[class*='cmp']",
      "[id*='consent']", "[class*='consent']",
      "[id*='tcf']", "[class*='tcf']",
      // иногда CMP кладёт root в body прямым ребёнком
      "body > div[style*='z-index']"
    ];

    for (const sel of selectors) {
      const nodes = Array.from(document.querySelectorAll(sel));
      for (const n of nodes) {
        if (looksLikeOverlay(n)) {
          hide(n);
          removed.push(sel);
        }
      }
    }

    // full screen backdrops (затемнение)
    const divs = Array.from(document.querySelectorAll("div")).slice(0, 3000);
    for (const d of divs) {
      const s = getComputedStyle(d);
      if (s.position !== "fixed") continue;
      const r = d.getBoundingClientRect();
      const full = r.width >= window.innerWidth * 0.95 && r.height >= window.innerHeight * 0.95;
      const z = parseFloat(s.zIndex || "0");
      const darkish = (s.backgroundColor && s.backgroundColor !== "rgba(0, 0, 0, 0)");
      if (full && z >= 100 && darkish) {
        hide(d);
        removed.push("fullscreen-backdrop");
      }
    }

    // restore scroll
    document.documentElement.style.setProperty("overflow", "auto", "important");
    document.body.style.setProperty("overflow", "auto", "important");

    return { ok: true, removedCount: removed.length };
  } catch (e) {
    return { ok: false, error: String(e && e.message ? e.message : e) };
  }
})()
`
		var res struct {
			OK           bool   `json:"ok"`
			RemovedCount int    `json:"removedCount"`
			Error        string `json:"error"`
		}
		_ = chromedp.Evaluate(js, &res).Do(ctx)
		if res.OK && res.RemovedCount > 0 {
			log.Debug().Int("removedCount", res.RemovedCount).Msg("stripConsentOverlay: overlay hidden")
		}
		return nil
	})
}

func waitAndClickRecapTab() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		js := `
(() => {
  const text = "recap";
  const candidates = [];
  const els = Array.from(document.querySelectorAll('[role="tab"], .q-tab'));
  for (const el of els) {
    const label = el.querySelector(".q-tab__label") || el;
    const t = (label.innerText || label.textContent || "").trim().toLowerCase();
    if (!t) continue;
    if (t === text || t.includes("recap")) {
      const r = el.getBoundingClientRect();
      const visible = r.width > 0 && r.height > 0;
      if (visible) candidates.push(el);
    }
  }
  let target = candidates.find(e => ((e.innerText||e.textContent||"").trim().toLowerCase() === text));
  if (!target) target = candidates[0];
  if (!target) return { ok: false, reason: "Recap tab not found in DOM" };
  target.click();
  return { ok: true };
})()
`
		deadline := time.Now().Add(20 * time.Second)
		var lastErr error

		for time.Now().Before(deadline) {
			var res struct {
				OK     bool   `json:"ok"`
				Reason string `json:"reason"`
			}
			err := chromedp.Evaluate(js, &res).Do(ctx)
			if err == nil && res.OK {
				return nil
			}
			if err != nil {
				lastErr = err
			} else if !res.OK {
				lastErr = errors.New(res.Reason)
			}
			time.Sleep(300 * time.Millisecond)
		}

		return dumpDebug(ctx, fmt.Sprintf("failed to click Recap tab: %v", lastErr))
	})
}

func waitRecapWrapperRobust(maxWait time.Duration, selector *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if selector != nil {
			*selector = "#recap-wrapper"
		}

		deadline := time.Now().Add(maxWait)

		for time.Now().Before(deadline) {
			var exists bool
			err := chromedp.Evaluate(`!!document.querySelector("#recap-wrapper")`, &exists).Do(ctx)
			if err == nil && exists {
				return nil
			}

			_ = chromedp.Run(ctx,
				waitAndClickRecapTab(),
				chromedp.Sleep(350*time.Millisecond),
			)

			time.Sleep(250 * time.Millisecond)
		}

		return dumpDebug(ctx, "recap wrapper not found after wait")
	})
}

// waitRecapAssetsLoadedSync: promise-free polling.
// Handles both <img> and Quasar q-img (background-image) rendering.
func waitRecapAssetsLoadedSync(maxWait time.Duration, selector *string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		sel := "#recap-wrapper"
		if selector != nil && *selector != "" {
			sel = *selector
		}

		js := fmt.Sprintf(`
(() => {
  try {
    const wrap = document.querySelector(%q);
    if (!wrap) return { ok: false, stage: "wrap_missing", reason: "wrap_not_found" };

    const container = wrap.querySelector("#recap-container");
    if (!container) return { ok: false, stage: "container_missing", reason: "recap_container_not_found" };

    const bg = (getComputedStyle(wrap).backgroundImage || "").trim();
    const hasBg = bg && bg !== "none";

    const imgs = Array.from(wrap.querySelectorAll("img"));
    const imgCount = imgs.length;

    let pending = 0;
    for (const im of imgs) {
      const complete = im.complete === true;
      const good = complete && (im.naturalWidth || 0) > 0;
      if (!good) pending++;
    }

    // Quasar q-img / CSS backgrounds
    const nodes = Array.from(wrap.querySelectorAll("div, span")).slice(0, 2500);
    let bgHits = 0;
    for (const el of nodes) {
      const bi = (getComputedStyle(el).backgroundImage || "").trim();
      if (!bi || bi === "none") continue;
      if (bi.includes("teamlogos") || bi.includes("stadiums") || bi.includes("mediafiles/uploads")) {
        const r = el.getBoundingClientRect();
        if (r.width > 0 && r.height > 0) bgHits++;
      }
      if (bgHits >= 2) break;
    }

    const hasVisualAssets = (imgCount > 0 && pending === 0) || (bgHits >= 2);

    if (!hasVisualAssets) {
      return { ok: false, stage: "assets_missing", reason: "no_loaded_imgs_and_no_bg_hits", imgCount, pending, bgHits, hasBg };
    }

    return { ok: true, stage: "done", reason: "ok", imgCount, pending, bgHits, hasBg };
  } catch (e) {
    return { ok: false, stage: "js_error", reason: String(e && e.message ? e.message : e) };
  }
})()
`, sel)

		deadline := time.Now().Add(maxWait)

		lastStage := "init"
		lastReason := "starting"
		lastImgCount := -1
		lastPending := -1
		lastBgHits := -1
		lastHasBg := false

		for time.Now().Before(deadline) {
			// CMP может всплыть “позже” — регулярно чистим
			_ = chromedp.Run(ctx, stripConsentOverlay())

			var res struct {
				OK      bool   `json:"ok"`
				Stage   string `json:"stage"`
				Reason  string `json:"reason"`
				ImgCnt  int    `json:"imgCount"`
				Pending int    `json:"pending"`
				BgHits  int    `json:"bgHits"`
				HasBg   bool   `json:"hasBg"`
			}

			err := chromedp.Evaluate(js, &res).Do(ctx)
			if err != nil {
				lastStage = "eval_error"
				lastReason = err.Error()
				time.Sleep(250 * time.Millisecond)
				continue
			}

			if res.Stage != "" {
				lastStage = res.Stage
			}
			if res.Reason != "" {
				lastReason = res.Reason
			}
			lastImgCount = res.ImgCnt
			lastPending = res.Pending
			lastBgHits = res.BgHits
			lastHasBg = res.HasBg

			if res.OK {
				return nil
			}

			time.Sleep(250 * time.Millisecond)
		}

		return dumpDebug(ctx, fmt.Sprintf(
			"timeout waiting recap assets: stage=%s reason=%s imgCount=%d pending=%d bgHits=%d hasBg=%v",
			lastStage, lastReason, lastImgCount, lastPending, lastBgHits, lastHasBg,
		))
	})
}

func dumpDebug(ctx context.Context, reason string) error {
	var (
		title   string
		loc     string
		snippet string
		fullPNG []byte
	)

	_ = chromedp.Title(&title).Do(ctx)
	_ = chromedp.Location(&loc).Do(ctx)

	_ = chromedp.Evaluate(`
(() => {
  const t = (document.body && (document.body.innerText || "")) || "";
  return t.slice(0, 1200);
})()
`, &snippet).Do(ctx)

	_ = chromedp.FullScreenshot(&fullPNG, 90).Do(ctx)
	if len(fullPNG) > 0 {
		_ = os.WriteFile("/tmp/neons_debug_full.png", fullPNG, 0o644)
	}

	log.Error().
		Str("reason", reason).
		Str("title", title).
		Str("location", loc).
		Str("snippet", snippet).
		Msg("neonsportz debug (saved /tmp/neons_debug_full.png)")

	return fmt.Errorf("%s (title=%q location=%s)", reason, title, loc)
}

func pageDebug(ctx context.Context) (string, error) {
	var title string
	if err := chromedp.Title(&title).Do(ctx); err != nil {
		return "", err
	}
	var bodyText string
	if err := chromedp.Evaluate(`document.body ? (document.body.innerText || "") : ""`, &bodyText).Do(ctx); err != nil {
		return "", err
	}
	re := regexp.MustCompile(`(?i)\bRecap\b`)
	hasRecap := re.MatchString(bodyText)
	return fmt.Sprintf(`title=%q recapTextPresent=%v (if false: recap hidden or game not completed)`, title, hasRecap), nil
}

// -------------------------
// URL helpers
// -------------------------

func buildGameURL(inURL, league, gameID string) (gameURL, l, g string, err error) {
	if strings.TrimSpace(inURL) != "" {
		u, e := url.Parse(inURL)
		if e != nil {
			return "", "", "", e
		}
		if u.Scheme == "" {
			return "", "", "", fmt.Errorf("url must include scheme (https://...)")
		}
		l, g = parseLeagueGameFromPath(u.Path)
		if l == "" || g == "" {
			return u.String(), "", "", fmt.Errorf("cannot parse league/game from url path; provide -league and -game too")
		}
		return u.String(), l, g, nil
	}

	league = strings.TrimSpace(league)
	gameID = strings.TrimSpace(gameID)
	if league == "" || gameID == "" {
		return "", "", "", fmt.Errorf("provide either -url or both -league and -game")
	}
	gameURL = fmt.Sprintf("https://neonsportz.com/leagues/%s/games/%s", url.PathEscape(league), url.PathEscape(gameID))
	return gameURL, league, gameID, nil
}

func parseLeagueGameFromPath(p string) (league, game string) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) >= 4 && parts[0] == "leagues" && parts[2] == "games" {
		return parts[1], parts[3]
	}
	return "", ""
}
