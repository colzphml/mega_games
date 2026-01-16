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
	defaultTimeout    = 140 * time.Second
	defaultAPITimeout = 15 * time.Second
	defaultAssetsWait = 60 * time.Second

	defaultViewportW  = 2600
	defaultViewportH  = 1500
	defaultHeadless   = true
	defaultSleepAfter = 0 * time.Second

	defaultScreenshotDir = "../screens"

	defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130 Safari/537.36"
)

var log = zerolog.New(os.Stdout).With().Str("package", "gochrome").Timestamp().Logger()

// Client renders recap images from NeonSportz web UI via headless Chrome (chromedp).
//
// Strategy (robust, download-like):
// - Load game page
// - Switch to "Recap" tab
// - Hide/remove cookie overlays (WITHOUT accepting cookies)
// - Remove fixed app chrome (header/toolbars) so it can't overlay recap
// - Wait recap assets (supports <img> and CSS background images)
// - Ensure stadium background is applied to #recap-wrapper (some pages keep it outside wrapper)
// - Wait stadium bg image actually loads (preload via Image.onload)
// - Screenshot #recap-wrapper (this matches the exported card styling best)
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
	pngBytes, err := screenshotRecapWrapper(ctx, gameURL, defaultTimeout, defaultHeadless, defaultSleepAfter, defaultAssetsWait, defaultViewportW, defaultViewportH)
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
// Headless Chrome screenshot (wrapper-based)
// -------------------------

func screenshotRecapWrapper(
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
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(parentCtx, allocOpts...)
	defer cancel()

	ctx, cancel2 := context.WithTimeout(allocCtx, overallTimeout)
	defer cancel2()

	ctx, cancel3 := chromedp.NewContext(ctx)
	defer cancel3()

	var (
		pngBytes []byte
		stage    string
	)

	setStage := func(name string) chromedp.ActionFunc {
		return func(ctx context.Context) error {
			stage = name
			log.Debug().Str("stage", stage).Msg("gochrome step")
			return nil
		}
	}

	const recapWrapperSel = "#recap-wrapper"

	tasks := chromedp.Tasks{
		setStage("emulation"),
		emulation.SetDeviceMetricsOverride(int64(viewportW), int64(viewportH), 1.0, false),
		emulation.SetUserAgentOverride(defaultUserAgent),

		setStage("network"),
		network.Enable(),
		network.SetBlockedURLs(cmpBlockedURLPatterns()),

		setStage("navigate"),
		chromedp.Navigate(gameURL),
		chromedp.WaitReady("body", chromedp.ByQuery),

		setStage("strip consent overlay"),
		stripConsentOverlay(),

		setStage("recap tab"),
		waitAndClickRecapTab(),

		setStage("recap wrapper"),
		waitRecapWrapperRobust(assetsWait),

		setStage("remove app chrome"),
		removeAppChrome(),

		setStage("assets"),
		waitRecapAssetsLoadedSync(assetsWait),

		// Stadium background: some pages keep it outside wrapper, so we inject it into wrapper.
		// IMPORTANT: we then WAIT for the background image to actually load, otherwise you may see
		// a "half-rendered" band at the top.
		setStage("inject stadium background"),
		ensureStadiumBackground(),

		setStage("wait stadium"),
		waitStadiumReady(8 * time.Second),

		setStage("scroll into view"),
		chromedp.ScrollIntoView(recapWrapperSel, chromedp.ByQuery),

		setStage("pre-screenshot cleanup"),
		stripConsentOverlay(),
		removeAppChrome(),
		ensureStadiumBackground(),
		waitStadiumReady(4 * time.Second),

		setStage("sleep after"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if sleepAfter > 0 {
				time.Sleep(sleepAfter)
			}
			return nil
		}),

		setStage("screenshot"),
		chromedp.Screenshot(recapWrapperSel, &pngBytes, chromedp.NodeVisible, chromedp.ByQuery),
	}

	if err := chromedp.Run(ctx, tasks); err != nil {
		return nil, fmt.Errorf("stage %s: %w", stage, err)
	}
	if len(pngBytes) == 0 {
		return nil, fmt.Errorf("stage %s: empty wrapper screenshot", stage)
	}
	return pngBytes, nil
}

// -------------------------
// CMP blocking + overlay removal (NO accept)
// -------------------------

func cmpBlockedURLPatterns() []string {
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
		"*://*/*iab*consent*",
		"*://*/*tcf*",
		"*://*/*gdpr*consent*",
	}
}

func stripConsentOverlay() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		js := `
(() => {
  try {
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
      "body > div[style*='z-index']"
    ];

    for (const sel of selectors) {
      const nodes = Array.from(document.querySelectorAll(sel));
      for (const n of nodes) {
        if (looksLikeOverlay(n)) hide(n);
      }
    }

    // Big dark full-screen overlays
    const divs = Array.from(document.querySelectorAll("div")).slice(0, 3500);
    for (const d of divs) {
      const s = getComputedStyle(d);
      if (s.position !== "fixed") continue;
      const r = d.getBoundingClientRect();
      const full = r.width >= window.innerWidth * 0.95 && r.height >= window.innerHeight * 0.95;
      const z = parseFloat(s.zIndex || "0");
      const darkish = (s.backgroundColor && s.backgroundColor !== "rgba(0, 0, 0, 0)");
      if (full && z >= 100 && darkish) hide(d);
    }

    document.documentElement.style.setProperty("overflow", "auto", "important");
    document.body.style.setProperty("overflow", "auto", "important");
    return true;
  } catch (_) { return false; }
})()
`
		var ok bool
		_ = chromedp.Evaluate(js, &ok).Do(ctx)
		return nil
	})
}

// removeAppChrome removes fixed Quasar header/toolbars so they can't overlay recap.
func removeAppChrome() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		js := `
(() => {
  try {
    const selectors = [
      "header",
      ".q-header",
      ".q-layout__section--marginal",
      ".q-toolbar",
      "[role='banner']"
    ];
    for (const sel of selectors) {
      document.querySelectorAll(sel).forEach(el => el.remove());
    }
    document.body.style.paddingTop = "0";
    document.documentElement.style.paddingTop = "0";
    return true;
  } catch (_) { return false; }
})()
`
		var ok bool
		_ = chromedp.Evaluate(js, &ok).Do(ctx)
		return nil
	})
}

// ensureStadiumBackground injects stadium background into #recap-wrapper if the page
// keeps it outside wrapper (e.g., on parent container or via pseudo-elements).
// It stores the chosen stadium URL in data-stadium-url for waitStadiumReady().
func ensureStadiumBackground() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		js := `
(() => {
  try {
    const wrap = document.querySelector("#recap-wrapper");
    if (!wrap) return { ok:false, reason:"wrap_missing" };

    const extractUrl = (bg) => {
      if (!bg || bg === "none") return "";
      const m = bg.match(/url\((['"]?)(.*?)\1\)/i);
      return (m && m[2]) ? m[2] : "";
    };

    // If wrapper already has url bg (maybe set by app), keep it but record URL.
    const wStyle = getComputedStyle(wrap);
    const existingUrl = extractUrl(wStyle.backgroundImage);
    if (existingUrl) {
      wrap.setAttribute("data-stadium-url", existingUrl);
      // don't assume it's loaded; waitStadiumReady will confirm
      return { ok:true, injected:false, url: existingUrl };
    }

    // Find best stadium-ish URL in visible DOM.
    const nodes = Array.from(document.querySelectorAll("div, section, main")).slice(0, 6000);
    let bestUrl = "";
    let bestArea = 0;

    for (const el of nodes) {
      const s = getComputedStyle(el);
      const bg = (s.backgroundImage || "").trim();
      if (!bg || bg === "none") continue;

      if (!(bg.includes("stadium") || bg.includes("stadiums") || bg.includes("mediafiles") || bg.includes("/uploads/"))) continue;

      const url = extractUrl(bg);
      if (!url) continue;

      const r = el.getBoundingClientRect();
      const area = r.width * r.height;
      if (r.width > 300 && r.height > 200 && area > bestArea) {
        bestArea = area;
        bestUrl = url;
      }
    }

    if (!bestUrl) {
      // last resort: body/html
      const b1 = extractUrl(getComputedStyle(document.body).backgroundImage);
      const b2 = extractUrl(getComputedStyle(document.documentElement).backgroundImage);
      bestUrl = b1 || b2;
    }

    if (!bestUrl) return { ok:false, reason:"stadium_bg_not_found" };

    wrap.setAttribute("data-stadium-url", bestUrl);
    wrap.setAttribute("data-stadium-ready", "0");

    // Apply with subtle dark overlay (close to site look)
    wrap.style.setProperty(
      "background-image",
      'linear-gradient(rgba(0,0,0,0.35), rgba(0,0,0,0.35)), url("' + bestUrl + '")',
      "important"
    );
    wrap.style.setProperty("background-size", "cover", "important");
    wrap.style.setProperty("background-position", "center center", "important");
    wrap.style.setProperty("background-repeat", "no-repeat", "important");
    wrap.style.setProperty("background-color", "#000", "important");

    return { ok:true, injected:true, url:bestUrl };
  } catch (e) {
    return { ok:false, reason:String(e && e.message ? e.message : e) };
  }
})()
`
		var res struct {
			OK       bool   `json:"ok"`
			Injected bool   `json:"injected"`
			URL      string `json:"url"`
			Reason   string `json:"reason"`
		}
		_ = chromedp.Evaluate(js, &res).Do(ctx)
		if res.OK && res.URL != "" {
			log.Debug().Str("stadiumUrl", res.URL).Bool("injected", res.Injected).Msg("stadium background prepared")
		}
		return nil
	})
}

// waitStadiumReady preloads the chosen stadium URL (stored in #recap-wrapper[data-stadium-url])
// and waits for Image.onload. This prevents "partially rendered" bands in screenshot.
func waitStadiumReady(maxWait time.Duration) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		js := `
(async () => {
  const wrap = document.querySelector("#recap-wrapper");
  if (!wrap) return { ok:false, reason:"wrap_missing" };

  const url = wrap.getAttribute("data-stadium-url") || "";
  if (!url) return { ok:true, skipped:true, reason:"no_stadium_url" };

  if (wrap.getAttribute("data-stadium-ready") === "1") {
    return { ok:true, ready:true, cached:true };
  }

  const ok = await new Promise((resolve) => {
    const img = new Image();
    img.onload = () => resolve(true);
    img.onerror = () => resolve(false);
    img.src = url;
  });

  wrap.setAttribute("data-stadium-ready", ok ? "1" : "0");
  return { ok: ok, ready: ok };
})()
`
		deadline := time.Now().Add(maxWait)
		for time.Now().Before(deadline) {
			var res struct {
				OK      bool   `json:"ok"`
				Ready   bool   `json:"ready"`
				Skipped bool   `json:"skipped"`
				Reason  string `json:"reason"`
			}
			_ = chromedp.Evaluate(js, &res).Do(ctx)

			if res.Skipped {
				// nothing to wait for
				return nil
			}
			if res.OK && res.Ready {
				return nil
			}
			time.Sleep(250 * time.Millisecond)
		}

		// Not fatal: still proceed with screenshot
		log.Warn().Msg("stadium background not confirmed loaded before screenshot (continuing)")
		return nil
	})
}

// -------------------------
// Recap navigation + readiness
// -------------------------

func waitAndClickRecapTab() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		js := `
(() => {
  const candidates = [];
  const els = Array.from(document.querySelectorAll('[role="tab"], .q-tab'));
  for (const el of els) {
    const label = el.querySelector(".q-tab__label") || el;
    const t = (label.innerText || label.textContent || "").trim().toLowerCase();
    if (!t) continue;
    if (t === "recap" || t.includes("recap")) {
      const r = el.getBoundingClientRect();
      if (r.width > 0 && r.height > 0) candidates.push(el);
    }
  }
  const target = candidates[0];
  if (!target) return { ok:false, reason:"Recap tab not found" };
  target.click();
  return { ok:true };
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
			} else {
				lastErr = errors.New(res.Reason)
			}
			time.Sleep(300 * time.Millisecond)
		}
		return fmt.Errorf("failed to click Recap tab: %v", lastErr)
	})
}

func waitRecapWrapperRobust(maxWait time.Duration) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		deadline := time.Now().Add(maxWait)
		for time.Now().Before(deadline) {
			var exists bool
			err := chromedp.Evaluate(`!!document.querySelector("#recap-wrapper")`, &exists).Do(ctx)
			if err == nil && exists {
				return nil
			}
			_ = chromedp.Run(ctx, waitAndClickRecapTab(), chromedp.Sleep(350*time.Millisecond))
			time.Sleep(250 * time.Millisecond)
		}
		return fmt.Errorf("recap wrapper not found")
	})
}

// waitRecapAssetsLoadedSync: promise-free polling.
// Works even if images are rendered as CSS backgrounds (Quasar q-img).
func waitRecapAssetsLoadedSync(maxWait time.Duration) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		js := `
(() => {
  try {
    const wrap = document.querySelector("#recap-wrapper");
    if (!wrap) return { ok:false, stage:"wrap_missing" };

    const container = wrap.querySelector("#recap-container");
    if (!container) return { ok:false, stage:"container_missing" };

    // Regular <img> tags
    const imgs = Array.from(wrap.querySelectorAll("img"));
    let pending = 0;
    for (const im of imgs) {
      const good = im.complete === true && (im.naturalWidth||0) > 0;
      if (!good) pending++;
    }

    // CSS background images (Quasar q-img often uses div with bg-image)
    const nodes = Array.from(wrap.querySelectorAll("div, span")).slice(0, 4000);
    let bgHits = 0;
    for (const el of nodes) {
      const bi = (getComputedStyle(el).backgroundImage || "").trim();
      if (!bi || bi === "none") continue;
      if (bi.includes("teamlogos") || bi.includes("stadiums") || bi.includes("mediafiles/uploads") || bi.includes("/uploads/")) {
        const r = el.getBoundingClientRect();
        if (r.width > 0 && r.height > 0) bgHits++;
      }
      if (bgHits >= 2) break;
    }

    const ok = (imgs.length > 0 && pending === 0) || (bgHits >= 2);
    return { ok, stage: ok ? "done":"waiting", imgCount: imgs.length, pending, bgHits };
  } catch (e) {
    return { ok:false, stage:"js_error", reason:String(e && e.message ? e.message : e) };
  }
})()
`
		deadline := time.Now().Add(maxWait)
		var last string
		for time.Now().Before(deadline) {
			_ = stripConsentOverlay().Do(ctx)

			var res struct {
				OK      bool   `json:"ok"`
				Stage   string `json:"stage"`
				Reason  string `json:"reason"`
				ImgCnt  int    `json:"imgCount"`
				Pending int    `json:"pending"`
				BgHits  int    `json:"bgHits"`
			}
			if err := chromedp.Evaluate(js, &res).Do(ctx); err != nil {
				last = "eval_error: " + err.Error()
				time.Sleep(250 * time.Millisecond)
				continue
			}
			if res.OK {
				return nil
			}
			last = fmt.Sprintf("stage=%s img=%d pending=%d bgHits=%d reason=%s", res.Stage, res.ImgCnt, res.Pending, res.BgHits, res.Reason)
			time.Sleep(250 * time.Millisecond)
		}
		return fmt.Errorf("timeout waiting recap assets: %s", last)
	})
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
			return u.String(), "", "", fmt.Errorf("cannot parse league/game from url path")
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
	// Expect: ["leagues", "<L>", "games", "<G>"]
	if len(parts) >= 4 && parts[0] == "leagues" && parts[2] == "games" {
		return parts[1], parts[3]
	}
	return "", ""
}
