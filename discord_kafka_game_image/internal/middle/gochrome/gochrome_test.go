package gochrome

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/types"
)

// -------------------------------------------------------------------
// mergeCancel: fast, deterministic, no Chrome involved. This is the
// mechanism that lets Fetch's caller-supplied context stop a render
// without ever being able to reach (and cancel) the long-lived browser
// context. See the doc comment on mergeCancel in gochrome.go for the
// reasoning; these tests pin the four properties that reasoning depends
// on.
// -------------------------------------------------------------------

func TestMergeCancelTriggerCancelsResult(t *testing.T) {
	base := context.Background()
	trigger, triggerCancel := context.WithCancel(context.Background())
	defer triggerCancel()

	merged, cancel := mergeCancel(base, trigger)
	defer cancel()

	select {
	case <-merged.Done():
		t.Fatal("merged context must not be done before trigger is cancelled")
	default:
	}

	triggerCancel()

	select {
	case <-merged.Done():
	case <-time.After(time.Second):
		t.Fatal("merged context was not cancelled within 1s of trigger being cancelled")
	}
	if !errors.Is(merged.Err(), context.Canceled) {
		t.Errorf("merged.Err() = %v, want context.Canceled", merged.Err())
	}
}

func TestMergeCancelNeverCancelsBase(t *testing.T) {
	base, baseCancel := context.WithCancel(context.Background())
	defer baseCancel()
	trigger := context.Background()

	_, cancel := mergeCancel(base, trigger)
	cancel() // cancel the derived (merged) context, not base

	select {
	case <-base.Done():
		t.Fatal("cancelling the merged context must never cancel base — that would let a " +
			"render's caller cancel the shared browser out from under other callers")
	default:
	}
}

func TestMergeCancelInheritsBaseCancellation(t *testing.T) {
	base, baseCancel := context.WithCancel(context.Background())
	trigger := context.Background()

	merged, cancel := mergeCancel(base, trigger)
	defer cancel()

	baseCancel()

	select {
	case <-merged.Done():
	case <-time.After(time.Second):
		t.Fatal("cancelling base must cancel the merged context: it is meant to be a real " +
			"context.WithCancel descendant of base, so that cancelling the browser cancels every render")
	}
}

func TestMergeCancelPreservesBaseValues(t *testing.T) {
	type key struct{}
	base := context.WithValue(context.Background(), key{}, "hello")
	trigger := context.Background()

	merged, cancel := mergeCancel(base, trigger)
	defer cancel()

	if got, _ := merged.Value(key{}).(string); got != "hello" {
		t.Errorf(`merged.Value(key{}) = %q, want "hello": chromedp.NewContext (FromContext) `+
			"walks the parent chain to find the already-allocated browser; if mergeCancel broke "+
			"that chain, every render would silently launch a brand new Chrome process instead "+
			"of attaching a tab to the shared one", got)
	}
}

func TestMergeCancelFiresImmediatelyIfTriggerAlreadyDone(t *testing.T) {
	base := context.Background()
	trigger, triggerCancel := context.WithCancel(context.Background())
	triggerCancel() // already done before mergeCancel is even called

	merged, cancel := mergeCancel(base, trigger)
	defer cancel()

	select {
	case <-merged.Done():
	case <-time.After(time.Second):
		t.Fatal("merged must already be done when trigger was already done before mergeCancel ran")
	}
}

// -------------------------------------------------------------------
// runBounded: also fast and Chrome-free. This backs warmUpBrowser's fix
// for the reviewer-flagged gap — chromedp's warm-up Run has no timeout of
// its own for the post-dial CDP handshake (see warmUpBrowser's doc
// comment) — without needing to actually reproduce a hung Chrome process.
// -------------------------------------------------------------------

func TestRunBoundedReturnsFnResultWhenFastEnough(t *testing.T) {
	if err := runBounded(time.Second, func() error { return nil }); err != nil {
		t.Errorf("runBounded = %v, want nil", err)
	}

	wantErr := errors.New("boom")
	if err := runBounded(time.Second, func() error { return wantErr }); !errors.Is(err, wantErr) {
		t.Errorf("runBounded = %v, want %v", err, wantErr)
	}
}

func TestRunBoundedTimesOutWithoutWaitingForSlowFn(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) }) // let the abandoned goroutine finish instead of leaking it

	start := time.Now()
	err := runBounded(50*time.Millisecond, func() error {
		<-release
		return nil
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error when fn outlives the timeout")
	}
	if elapsed > time.Second {
		t.Errorf("runBounded took %s to give up on a stuck fn, want close to the 50ms timeout", elapsed)
	}
}

// -------------------------------------------------------------------
// Real-Chrome integration tests. These are the empirical proof the task
// asked for: a real chromedp-driven Chrome against an httptest stand-in
// for NeonSportz. They are slow (each spins up a Chrome process) and are
// skipped under -short, matching the existing convention for
// container-backed tests in this repo (see storage/minio_test.go).
// -------------------------------------------------------------------

// recapPageHTML returns a minimal, self-contained page that satisfies
// screenshotRecapWrapper's pipeline without depending on any other
// network fetch: a role="tab" element containing the text "Recap" (so the
// unconditional "recap tab" stage — waitAndClickRecapTab — finds
// something to click even though nothing on this static page actually
// reacts to the click), #recap-wrapper present in the initial DOM (so the
// following waitRecapWrapperRobust check succeeds on its first poll), and
// #recap-container plus one real (data-URI) <img> inside it so
// waitRecapAssetsLoadedSync's readiness check passes. No stadium
// background is included; ensureStadiumBackground/waitStadiumReady both
// treat that as "nothing to do" rather than an error (see their comments
// in gochrome.go), so omitting it does not affect success.
func recapPageHTML(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode placeholder png: %v", err)
	}
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	return `<!doctype html>
<html><head><meta charset="utf-8"><title>recap</title></head>
<body>
<div role="tab" style="width:80px;height:24px;">Recap</div>
<div id="recap-wrapper" style="width:400px;height:300px;background:#223344;">
  <div id="recap-container">
    <img src="` + dataURI + `" width="10" height="10" alt="logo">
  </div>
</div>
</body></html>`
}

// recapJSONBody is a minimal API precheck payload that passes Fetch's
// checks (non-zero pk, status above 1).
const recapJSONBody = `{"game":{"pk":1,"homeScore":10,"awayScore":7,"status":3,` +
	`"homeTeam":{"displayName":"Home"},"awayTeam":{"displayName":"Away"}}}`

// hangingHandler never writes a response until release is closed (or the
// test server itself is shut down), simulating a page load that never
// completes. Blocking on release (controlled directly by the test)
// instead of solely on the request's own context avoids any risk of
// httptest.Server.Close hanging if chromedp/Chrome does not tear down the
// underlying TCP connection the instant its context is cancelled.
func hangingHandler(release <-chan struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
}

func newChromeTestClient(t *testing.T, mux *http.ServeMux) (client *Client, srv *httptest.Server, release chan struct{}) {
	t.Helper()
	if testing.Short() {
		t.Skip("spins up a real Chrome process; skipped in -short mode")
	}

	release = make(chan struct{})
	srv = httptest.NewServer(mux)
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	client = &Client{baseURL: srv.URL, league: "L", headless: true}
	t.Cleanup(func() { _ = client.Close() })

	// Warm the browser up front, synchronously, so the tests below measure
	// cancellation/Close latency in isolation from Chrome's cold-start
	// time. That start-up latency is real but highly variable — observed
	// anywhere from about a second to over ten seconds on this machine,
	// depending on system load — and is not itself what these tests are
	// about; conflating the two the first time around produced a false
	// failure (11s to react to a cancelled context, entirely spent in
	// warm-up before the render's own context tree was even in play).
	client.renderMu.Lock()
	_, err := client.ensureBrowserLocked()
	client.renderMu.Unlock()
	if err != nil {
		t.Fatalf("warm up browser: %v", err)
	}

	return client, srv, release
}

// TestRenderScreenshotStopsWhenCallerContextIsCancelled is the core proof
// for the regression: today Fetch's ctx parameter is never used, so a
// render only stops at its own baked-in stage timeouts (tens of seconds
// to defaultTimeout's 3 minutes), no matter what the caller does. Here
// the game page never responds at all, and the caller's context is given
// only ~1s to live — well under the 30s/10s stage budgets passed to
// renderScreenshot, which are themselves already far smaller than
// production's defaults. If the caller's cancellation is respected, this
// returns in around one second; if it is ignored (today's bug), it does
// not return until one of those stage budgets elapses, tens of seconds
// away.
func TestRenderScreenshotStopsWhenCallerContextIsCancelled(t *testing.T) {
	mux := http.NewServeMux()
	client, srv, release := newChromeTestClient(t, mux)
	mux.HandleFunc("/leagues/L/games/hang", hangingHandler(release))
	gameURL := srv.URL + "/leagues/L/games/hang"

	callCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	type result struct {
		png []byte
		err error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		png, err := client.renderScreenshot(callCtx, gameURL, 30*time.Second, 0, 10*time.Second, 800, 600)
		done <- result{png, err}
	}()

	select {
	case r := <-done:
		elapsed := time.Since(start)
		t.Logf("renderScreenshot returned after %s (err=%v)", elapsed, r.err)
		if r.err == nil {
			t.Fatalf("expected an error once the caller's context was cancelled, got a %d-byte image", len(r.png))
		}
		if elapsed > 5*time.Second {
			t.Errorf("renderScreenshot took %s to react to a 1s-lived caller context; want a "+
				"couple of seconds, nowhere near the 10s/30s stage budgets passed in", elapsed)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("renderScreenshot did not return within 40s of the caller's context expiring; " +
			"it looks like ctx is being ignored and the render is running out its own timeouts instead")
	}
}

// TestRenderSurvivesCallerCancellationAndReusesBrowser proves the other
// half of the requirement: cancelling one caller's render must not tear
// down the shared browser. It reuses the exact same *Client across a
// cancelled render and a normal one, and asserts the browser context
// pointer is identical afterwards (not merely that some browser exists) —
// renderScreenshot's error path only skips its defensive recycle when
// the failure was the caller's own cancellation (checked via ctx.Err()),
// so this also exercises that distinction against a real render, not just
// against the code path in isolation.
func TestRenderSurvivesCallerCancellationAndReusesBrowser(t *testing.T) {
	mux := http.NewServeMux()
	client, srv, release := newChromeTestClient(t, mux)
	mux.HandleFunc("/leagues/L/games/hang", hangingHandler(release))
	mux.HandleFunc("/leagues/L/games/ok", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, recapPageHTML(t))
	})

	// Phase 1: the caller gives up almost immediately.
	callCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	hangURL := srv.URL + "/leagues/L/games/hang"
	if _, err := client.renderScreenshot(callCtx, hangURL, 30*time.Second, 0, 10*time.Second, 800, 600); err == nil {
		t.Fatal("phase 1: expected the cancelled render to return an error")
	}

	client.browserMu.Lock()
	browserAfterCancel := client.browserCtx
	client.browserMu.Unlock()
	rendersAfterCancel := client.renders
	if browserAfterCancel == nil {
		t.Fatal("phase 1: a browser should have been allocated to attempt the render, cancelled or not")
	}

	// Phase 2: a normal render, same client, right after.
	okURL := srv.URL + "/leagues/L/games/ok"
	pngBytes, err := client.renderScreenshot(context.Background(), okURL, 30*time.Second, 0, 10*time.Second, 800, 600)
	if err != nil {
		t.Fatalf("phase 2: render after a cancelled call must still succeed, got: %v", err)
	}
	if _, decodeErr := png.Decode(bytes.NewReader(pngBytes)); decodeErr != nil {
		t.Errorf("phase 2: screenshot is not a valid PNG: %v", decodeErr)
	}

	client.browserMu.Lock()
	browserAfterSuccess := client.browserCtx
	client.browserMu.Unlock()
	rendersAfterSuccess := client.renders

	if browserAfterSuccess == nil || browserAfterSuccess != browserAfterCancel {
		t.Error("the browser context changed between phase 1 and phase 2: cancelling the caller's " +
			"render tore down (or otherwise replaced) the shared browser instead of leaving it alive " +
			"for reuse")
	}
	if rendersAfterSuccess != rendersAfterCancel+1 {
		t.Errorf("renders = %d after phase 2, want %d (phase 1's count, plus this one): a jump other "+
			"than +1 suggests the browser was recreated rather than reused",
			rendersAfterSuccess, rendersAfterCancel+1)
	}
}

// TestCloseReturnsPromptlyWhileRenderInProgress covers the second
// consequence described in the task: before this fix, Close only called
// c.mu.Lock(), which blocked until an in-flight render released it on its
// own — up to defaultTimeout (3 minutes) — because the render's context
// tree had no way to hear about Close. Docker Compose's default stop
// grace period is about ten seconds (this repo does not override it for
// this service), so that Close risked the container being killed
// outright rather than shutting down cleanly. Here the render runs
// against a page that never responds and is given no caller deadline at
// all (context.Background()); only Close cancelling the shared browser
// context can stop it.
func TestCloseReturnsPromptlyWhileRenderInProgress(t *testing.T) {
	mux := http.NewServeMux()
	client, srv, release := newChromeTestClient(t, mux)
	_ = release // Close, not release, is what must free this render — see below.
	mux.HandleFunc("/leagues/L/games/hang", hangingHandler(release))
	gameURL := srv.URL + "/leagues/L/games/hang"

	type result struct {
		png []byte
		err error
	}
	renderDone := make(chan result, 1)
	go func() {
		png, err := client.renderScreenshot(context.Background(), gameURL, 3*time.Minute, 0, time.Minute, 800, 600)
		renderDone <- result{png, err}
	}()

	// Give the render a moment to actually acquire the browser and start
	// navigating, so Close has real in-flight work to interrupt rather
	// than winning a race against a render that hasn't started yet. The
	// browser is already warm (newChromeTestClient), so reaching the
	// hanging page only takes a handful of fast local CDP round-trips.
	time.Sleep(500 * time.Millisecond)

	closeStart := time.Now()
	closeDone := make(chan error, 1)
	go func() { closeDone <- client.Close() }()

	select {
	case err := <-closeDone:
		elapsed := time.Since(closeStart)
		t.Logf("Close returned after %s (err=%v)", elapsed, err)
		if elapsed > 5*time.Second {
			t.Errorf("Close took %s while a render was in flight; want well under Docker "+
				"Compose's ~10s stop grace period", elapsed)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Close did not return within 20s of being called while a render was in flight; " +
			"it is waiting out the render instead of cancelling it")
	}

	select {
	case r := <-renderDone:
		if r.err == nil {
			t.Error("expected the in-flight render to fail once Close cancelled the shared browser")
		} else {
			t.Logf("in-flight render ended with: %v", r.err)
		}
	case <-time.After(20 * time.Second):
		t.Error("the in-flight render did not unwind within 20s of Close cancelling the browser")
	}
}

// TestFetchHonorsCallerContextCancellation exercises the exact function
// named in the regression report: Fetch's ctx parameter must actually
// reach the render, not just renderScreenshot when called directly (the
// bug was that Fetch never passed ctx anywhere). The metadata fallback
// route is deliberately left unregistered so it 404s: if Fetch's fallback
// path were reached it would fail fast and cleanly rather than
// masking the render's cancellation behind a degraded-but-successful
// result.
func TestFetchHonorsCallerContextCancellation(t *testing.T) {
	mux := http.NewServeMux()
	client, srv, release := newChromeTestClient(t, mux)
	_ = srv
	mux.HandleFunc("/api/leagues/L/games/hang/recap/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, recapJSONBody)
	})
	mux.HandleFunc("/leagues/L/games/hang", hangingHandler(release))

	callCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	type result struct {
		res types.Result
		err error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		res, err := client.Fetch(callCtx, "hang")
		done <- result{res, err}
	}()

	select {
	case r := <-done:
		elapsed := time.Since(start)
		t.Logf("Fetch returned after %s (err=%v, degraded=%v)", elapsed, r.err, r.res.Degraded)
		if r.err == nil {
			t.Error("Fetch with a cancelled context and no working fallback endpoint must return an error")
		}
		if elapsed > 5*time.Second {
			t.Errorf("Fetch took %s to react to a 1s-lived context; want a couple of seconds, "+
				"nowhere near the multi-minute defaults baked into the render", elapsed)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("Fetch did not return within 40s of its context expiring — its ctx parameter " +
			"appears to be ignored")
	}
}

// -------------------------------------------------------------------
// Fix round 1: a protocol race in the browserMu/renderMu split itself,
// found by review (not by -race — every access to the guarded fields is
// correctly synchronized; the bug is about *when* Close's intent becomes
// visible relative to creation publishing, not about unsynchronized
// memory access). See the "Fix round 1" comment on ensureBrowserLocked in
// gochrome.go for the mechanism.
// -------------------------------------------------------------------

// TestCloseDuringBrowserCreationDoesNotLeakTheBrowser reproduces the race
// directly: start a browser creation, give it only a few milliseconds'
// head start — nowhere near enough to actually finish spawning a Chrome
// process, reading its debugger address, dialing, and completing the CDP
// handshake, all of which take at least tens of milliseconds even on a
// fast, idle machine — then call Close. Before the fix, Close saw nothing
// published yet, reported success, and the creation that was already in
// flight published a browser afterwards that nothing would ever cancel
// again. Run several times, matching how this was actually found
// (reproduced repeatedly on demand, not once by luck): this is not
// expected to be timing-flaky given how large the margin is between "a
// few ms" and "how long a real Chrome allocation takes", but repetition
// is cheap insurance against this machine being unusually fast, and
// mirrors the review's own three-for-three reproduction.
func TestCloseDuringBrowserCreationDoesNotLeakTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("spins up real Chrome processes; skipped in -short mode")
	}

	for i := 0; i < 5; i++ {
		t.Run("race", func(t *testing.T) {
			client := &Client{headless: true}

			type createResult struct {
				ctx context.Context
				err error
			}
			done := make(chan createResult, 1)
			go func() {
				client.renderMu.Lock()
				ctx, err := client.ensureBrowserLocked()
				client.renderMu.Unlock()
				done <- createResult{ctx, err}
			}()

			time.Sleep(3 * time.Millisecond)

			if err := client.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			var created createResult
			select {
			case created = <-done:
				t.Logf("ensureBrowserLocked (racing Close): ctx-non-nil=%v err=%v", created.ctx != nil, created.err)
			case <-time.After(90 * time.Second):
				t.Fatal("ensureBrowserLocked racing Close did not finish in time")
			}

			client.browserMu.Lock()
			published := client.browserCtx
			client.browserMu.Unlock()

			if published != nil {
				t.Fatal("a browser is published after Close returned: Close reported success " +
					"without stopping (or preventing) this concurrently-created browser, so " +
					"nothing will ever cancel its process")
			}
		})
	}
}

// TestCloseTearsDownAnAlreadyPublishedBrowser is the first of the two
// "opposite" cases the fix must not break: Close on a client whose
// browser is already published and idle (no creation, no render racing
// it) must still tear it down cleanly, exactly as before this round's
// change.
func TestCloseTearsDownAnAlreadyPublishedBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("spins up a real Chrome process; skipped in -short mode")
	}

	client := &Client{headless: true}
	client.renderMu.Lock()
	_, err := client.ensureBrowserLocked()
	client.renderMu.Unlock()
	if err != nil {
		t.Fatalf("warm up browser: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	client.browserMu.Lock()
	browserCtx := client.browserCtx
	allocCtx := client.allocCtx
	closed := client.closed
	client.browserMu.Unlock()

	if browserCtx != nil || allocCtx != nil {
		t.Error("browser/allocator fields should be cleared after Close tears down a published browser")
	}
	if !closed {
		t.Error("closed should be set after Close")
	}
}

// TestEnsureBrowserLockedFailsCleanlyAfterClose is the second "opposite"
// case: creating a browser right after Close must fail fast and cleanly,
// not hang, and not spend a full Chrome startup only to throw the result
// away — the early c.closed check in ensureBrowserLocked exists
// specifically to avoid that waste, in addition to the late recheck
// covering the race itself.
func TestEnsureBrowserLockedFailsCleanlyAfterClose(t *testing.T) {
	if testing.Short() {
		t.Skip("would spin up a real Chrome process if the fast path were broken; skipped in -short mode")
	}

	client := &Client{headless: true}
	if err := client.Close(); err != nil {
		t.Fatalf("Close on a never-used client: %v", err)
	}

	start := time.Now()
	client.renderMu.Lock()
	ctx, err := client.ensureBrowserLocked()
	client.renderMu.Unlock()
	elapsed := time.Since(start)

	if !errors.Is(err, errClosed) {
		t.Errorf("ensureBrowserLocked after Close = %v, want errClosed", err)
	}
	if ctx != nil {
		t.Error("ensureBrowserLocked after Close must not return a usable browser context")
	}
	if elapsed > time.Second {
		t.Errorf("ensureBrowserLocked after Close took %s, want near-instant (the fast-path "+
			"closed check should short-circuit before ever touching Chrome)", elapsed)
	}
}
