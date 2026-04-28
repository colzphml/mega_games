package gochrome

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"github.com/rs/zerolog"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/types"
)

const (
	defaultTimeout     = 5 * time.Minute
	defaultAPITimeout  = 45 * time.Second
	defaultMetaTimeout = 45 * time.Second
	defaultAssetsWait  = 90 * time.Second

	defaultViewportW  = 2600
	defaultViewportH  = 1500
	defaultSleepAfter = 0 * time.Second

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
	baseURL       string
	league        string
	headless      bool
	screenshotDir string
}

func NewClient(ctx context.Context, cfg config.Config) (*Client, error) {
	return &Client{
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
		league:        strings.TrimSpace(cfg.League),
		headless:      cfg.GoChromeHeadless,
		screenshotDir: "",
	}, nil
}

func (c *Client) Close() error {
	log.Info().Msg("closing gochrome client")
	return nil
}

func (c *Client) Fetch(ctx context.Context, gameID string) (types.Result, error) {
	gameURL, league, gameID, err := buildGameURL(c.baseURL, c.league, gameID)
	if err != nil {
		return types.Result{}, err
	}

	apiURL := fmt.Sprintf("%s/api/leagues/%s/games/%s/recap/", c.baseURL, url.PathEscape(league), url.PathEscape(gameID))
	metaURL := fmt.Sprintf("%s/api/leagues/%s/games/%s/", c.baseURL, url.PathEscape(league), url.PathEscape(gameID))

	// 1) API precheck (fast fail)
	recap, err := fetchRecapJSON(apiURL, defaultUserAgent, defaultAPITimeout)
	if err != nil {
		if isRecoverablePrecheckError(err) {
			log.Warn().Err(err).Str("game_id", gameID).Msg("recap precheck degraded, fallback to metadata card")
			return buildFallbackResult(metaURL, gameURL, league, gameID)
		}
		return types.Result{}, fmt.Errorf("api precheck failed: %w", err)
	}
	if recap.Game.PK == 0 {
		return types.Result{}, fmt.Errorf("api precheck failed: recap.game.pk is empty (unexpected response)")
	}
	if recap.Game.Status <= 1 {
		return types.Result{}, fmt.Errorf("recap is not available: game.status=%d (usually means not completed yet)", recap.Game.Status)
	}

	// 2) Headless Chrome screenshot
	pngBytes, err := screenshotRecapWrapper(ctx, gameURL, defaultTimeout, c.headless, defaultSleepAfter, defaultAssetsWait, defaultViewportW, defaultViewportH)
	if err != nil {
		log.Warn().Err(err).Str("game_id", gameID).Msg("screenshot failed, fallback to metadata card")
		return buildFallbackResult(metaURL, gameURL, league, gameID)
	}
	if len(pngBytes) == 0 {
		return types.Result{}, fmt.Errorf("empty screenshot bytes")
	}

	if c.screenshotDir != "" {
		if saveErr := saveScreenshot(c.screenshotDir, gameID, pngBytes); saveErr != nil {
			log.Error().Err(saveErr).Str("dir", c.screenshotDir).Msg("failed to save screenshot")
		}
	}

	return types.Result{
		GameURL:     gameURL,
		ContentType: "image/png",
		Image:       pngBytes,
	}, nil
}

type GameMetaResponse struct {
	PK        int      `json:"pk"`
	WeekIndex int      `json:"weekIndex"`
	Status    int      `json:"status"`
	HomeScore int      `json:"homeScore"`
	AwayScore int      `json:"awayScore"`
	HomeTeam  TeamMeta `json:"homeTeam"`
	AwayTeam  TeamMeta `json:"awayTeam"`
}

type TeamMeta struct {
	DisplayName string `json:"displayName"`
	AbbrName    string `json:"abbrName"`
	CityName    string `json:"cityName"`
}

func buildFallbackResult(metaURL, gameURL, league, gameID string) (types.Result, error) {
	meta, err := fetchGameMetaJSON(metaURL, defaultUserAgent, defaultMetaTimeout)
	if err != nil {
		return types.Result{}, fmt.Errorf("fallback metadata fetch failed: %w", err)
	}
	imageBytes, err := renderFallbackCard(meta, league, gameID)
	if err != nil {
		return types.Result{}, fmt.Errorf("render fallback card: %w", err)
	}
	return types.Result{
		GameURL:     gameURL,
		ContentType: "image/png",
		Image:       imageBytes,
	}, nil
}

func fetchGameMetaJSON(apiURL, userAgent string, to time.Duration) (*GameMetaResponse, error) {
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("unexpected status %d from game API; body=%q", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	dec := json.NewDecoder(resp.Body)
	var meta GameMetaResponse
	if err := dec.Decode(&meta); err != nil {
		return nil, fmt.Errorf("decode game metadata: %w", err)
	}
	if meta.PK == 0 {
		return nil, fmt.Errorf("empty game metadata")
	}
	return &meta, nil
}

func renderFallbackCard(meta *GameMetaResponse, league, gameID string) ([]byte, error) {
	const (
		width  = 1600
		height = 900
	)

	dc := gg.NewContext(width, height)
	bg := gg.NewLinearGradient(0, 0, float64(width), float64(height))
	bg.AddColorStop(0, color.RGBA{18, 24, 34, 255})
	bg.AddColorStop(1, color.RGBA{38, 46, 62, 255})
	dc.SetFillStyle(bg)
	dc.DrawRectangle(0, 0, float64(width), float64(height))
	dc.Fill()

	dc.SetColor(color.RGBA{230, 236, 245, 255})
	dc.SetFontFace(scoreFont(44))
	dc.DrawStringAnchored(fmt.Sprintf("%s Recap", strings.ToUpper(strings.TrimSpace(league))), 90, 90, 0, 0.5)

	status := "Final"
	if meta.Status <= 1 {
		status = "In Progress"
	}
	dc.SetFontFace(scoreFont(26))
	dc.SetColor(color.RGBA{180, 192, 210, 255})
	dc.DrawStringAnchored(fmt.Sprintf("Week %d | Game %s | %s", meta.WeekIndex+1, gameID, status), 90, 140, 0, 0.5)

	home := teamLabel(meta.HomeTeam)
	away := teamLabel(meta.AwayTeam)

	dc.SetColor(color.RGBA{245, 248, 252, 255})
	dc.SetFontFace(scoreFont(70))
	dc.DrawStringAnchored(away, 320, 340, 0.5, 0.5)
	dc.DrawStringAnchored(home, 1280, 340, 0.5, 0.5)

	dc.SetColor(color.RGBA{255, 215, 106, 255})
	dc.SetFontFace(scoreFont(180))
	dc.DrawStringAnchored(fmt.Sprintf("%d", meta.AwayScore), 640, 530, 0.5, 0.5)
	dc.DrawStringAnchored("-", 800, 530, 0.5, 0.5)
	dc.DrawStringAnchored(fmt.Sprintf("%d", meta.HomeScore), 960, 530, 0.5, 0.5)

	dc.SetColor(color.RGBA{170, 182, 200, 255})
	dc.SetFontFace(scoreFont(24))
	dc.DrawStringAnchored("Fallback card: source recap endpoint timed out from current runtime network", float64(width)/2, 820, 0.5, 0.5)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func teamLabel(t TeamMeta) string {
	if v := strings.TrimSpace(t.AbbrName); v != "" {
		return strings.ToUpper(v)
	}
	if v := strings.TrimSpace(t.DisplayName); v != "" {
		return v
	}
	if v := strings.TrimSpace(t.CityName); v != "" {
		return v
	}
	return "TEAM"
}

func scoreFont(size float64) font.Face {
	tt, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return basicfont.Face7x13
	}
	return truetype.NewFace(tt, &truetype.Options{
		Size: size,
		DPI:  72,
	})
}

func isRecoverablePrecheckError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "client.timeout") || strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "unexpected eof")
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
		waitStadiumReady(10 * time.Second),

		setStage("scroll into view"),
		chromedp.ScrollIntoView(recapWrapperSel, chromedp.ByQuery),

		setStage("pre-screenshot cleanup"),
		stripConsentOverlay(),
		removeAppChrome(),
		ensureStadiumBackground(),
		waitStadiumReady(5 * time.Second),

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

func buildGameURL(baseURL, league, gameID string) (gameURL, l, g string, err error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	league = strings.TrimSpace(league)
	gameID = strings.TrimSpace(gameID)
	if baseURL == "" {
		return "", "", "", fmt.Errorf("missing base url")
	}
	if league == "" || gameID == "" {
		return "", "", "", fmt.Errorf("missing league or game id")
	}
	gameURL = fmt.Sprintf("%s/leagues/%s/games/%s", baseURL, url.PathEscape(league), url.PathEscape(gameID))
	return gameURL, league, gameID, nil
}
