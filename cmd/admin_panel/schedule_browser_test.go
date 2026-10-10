package main

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// This browser regression uses the pinned HTMX 1.9.10 script from a local file,
// so no CDN or database is needed. Set MEGA_GAMES_HTMX_SCRIPT to that file and
// run go test ./cmd/admin_panel -run TestScheduleUploadBrowser -count=1.
func TestScheduleUploadBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real Chrome process")
	}
	scriptPath := os.Getenv("MEGA_GAMES_HTMX_SCRIPT")
	if scriptPath == "" {
		t.Skip("set MEGA_GAMES_HTMX_SCRIPT to a local HTMX 1.9.10 script")
	}
	htmxScript, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	_, testFile, _, _ := runtime.Caller(0)
	templateDir := filepath.Join(filepath.Dir(testFile), "templates")
	tmpl, err := template.ParseFiles(filepath.Join(templateDir, "layout.html"), filepath.Join(templateDir, "schedule.html"))
	if err != nil {
		t.Fatal(err)
	}
	var page bytes.Buffer
	if err := tmpl.ExecuteTemplate(&page, "layout.html", nil); err != nil {
		t.Fatal(err)
	}
	html := strings.ReplaceAll(page.String(), "https://unpkg.com/htmx.org@1.9.10", "/htmx.js")
	cases := []struct {
		status int
		body   string
		text   string
	}{
		{400, `CSV row 3: invalid weekIndex: <img src=x onerror="window.uploadScriptExecuted=true">`, `CSV row 3: invalid weekIndex: <img src=x onerror="window.uploadScriptExecuted=true">`},
		{500, "Database error\n", "Database error\n"},
		{200, "<div>Successfully uploaded 1 games</div>", "Successfully uploaded 1 games"},
	}
	var attempts atomic.Int32
	started := make(chan int, len(cases))
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /schedule", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, html)
	})
	mux.HandleFunc("GET /htmx.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write(htmxScript)
	})
	mux.HandleFunc("POST /schedule/upload", func(w http.ResponseWriter, r *http.Request) {
		index := int(attempts.Add(1)) - 1
		started <- index
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		if index >= len(cases) {
			http.Error(w, "unexpected extra upload", 500)
			return
		}
		w.WriteHeader(cases[index].status)
		fmt.Fprint(w, cases[index].body)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	defer close(release)
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()
	ctx, stop := context.WithTimeout(ctx, 45*time.Second)
	defer stop()
	var version string
	if err := chromedp.Run(ctx,
		network.Enable(), network.SetBlockedURLs([]string{"https://*"}),
		chromedp.Navigate(server.URL+"/schedule"),
		chromedp.Evaluate(`htmx.version`, &version),
		chromedp.Evaluate(`window.completedUploads=0; document.body.addEventListener('htmx:afterRequest', () => window.completedUploads++);
			const files=new DataTransfer(); files.items.add(new File(['homeTeam,awayTeam,seasonIndex,stageIndex,weekIndex\nBills,Jets,0,1,7\n'], 'games.csv', {type:'text/csv'})); document.getElementById('file').files=files.files;`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if version != "1.9.10" {
		t.Fatalf("HTMX fixture version = %q, want 1.9.10", version)
	}
	for i, tc := range cases {
		if err := chromedp.Run(ctx, chromedp.Click("button[type=submit]", chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		select {
		case index := <-started:
			if index != i {
				t.Fatalf("upload index = %d, want %d", index, i)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("upload request did not arrive")
		}
		var busy string
		if err := chromedp.Run(ctx, chromedp.AttributeValue("button[type=submit]", "aria-busy", &busy, nil, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		if busy != "true" {
			t.Errorf("pending HTTP %d: aria-busy = %q, want true", tc.status, busy)
		}
		release <- struct{}{}
		var state struct {
			Text     string
			Busy     string
			Images   int
			Executed bool
		}
		if err := chromedp.Run(ctx,
			chromedp.Poll(fmt.Sprintf("window.completedUploads === %d", i+1), nil, chromedp.WithPollingTimeout(5*time.Second)),
			chromedp.Evaluate(`({Text:document.getElementById('upload-result').textContent, Busy:document.querySelector('button[type=submit]').getAttribute('aria-busy'), Images:document.querySelectorAll('#upload-result img').length, Executed:window.uploadScriptExecuted === true})`, &state),
		); err != nil {
			t.Fatal(err)
		}
		if state.Text != tc.text || state.Busy != "false" || state.Images != 0 || state.Executed {
			t.Errorf("after HTTP %d: state = %+v, want text %q, idle button and no HTML execution", tc.status, state, tc.text)
		}
	}
}
