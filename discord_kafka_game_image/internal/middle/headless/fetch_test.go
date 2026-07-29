package headless

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// readFixture loads the recorded NeonSportz recap payload shared by the
// tests in this file. It is a real API response (Patriots 31, Commanders
// 38), not a synthetic literal: NeonSportz mixes JSON numbers and JSON
// strings for numeric fields inconsistently across stat blocks, and only a
// fixture pinned from a live response can catch a regression in that mix.
func readFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "recap.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestJSONIntAcceptsNumberAndString(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{`5`, 5},
		{`"5"`, 5},
		{`"5.7"`, 6},
		{`5.4`, 5},
		{`null`, 0},
		{`""`, 0},
	}
	for _, tc := range cases {
		var got JSONInt
		if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
			t.Errorf("Unmarshal(%s): %v", tc.raw, err)
			continue
		}
		if int(got) != tc.want {
			t.Errorf("Unmarshal(%s) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

// TestJSONIntParsesRealFixtureStringField pins the exact reason JSONInt
// exists. In the recorded response, def_home_stats.defSacks arrives as the
// string "0.00" while def_home_stats.defTotalTackles, a few fields away in
// the same object, arrives as the plain number 3 — the same API,
// inconsistent even within one block. A plain `int` field would fail to
// decode this payload at all ("json: cannot unmarshal string into Go
// struct field ... of type int"); JSONInt must accept both forms.
func TestJSONIntParsesRealFixtureStringField(t *testing.T) {
	var rec Recap
	if err := json.Unmarshal(readFixture(t), &rec); err != nil {
		t.Fatalf("unmarshal real recap fixture: %v", err)
	}
	if rec.DefHomeStats == nil {
		t.Fatal("def_home_stats must be populated from the fixture")
	}
	if got := int(rec.DefHomeStats.DefSacks); got != 0 {
		t.Errorf("DefSacks (string field %q in the fixture) = %d, want 0", "0.00", got)
	}
	if got := int(rec.DefHomeStats.DefTotalTackles); got != 3 {
		t.Errorf("DefTotalTackles (numeric field in the same fixture block) = %d, want 3", got)
	}
}

func TestFetchJSONParsesRealRecap(t *testing.T) {
	fixture := readFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	got, err := fetchJSON(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("fetchJSON: %v", err)
	}

	// Teams.
	if got.Game.HomeTeam.DisplayName != "Commanders" {
		t.Errorf("home team = %q, want %q", got.Game.HomeTeam.DisplayName, "Commanders")
	}
	if got.Game.AwayTeam.DisplayName != "Patriots" {
		t.Errorf("away team = %q, want %q", got.Game.AwayTeam.DisplayName, "Patriots")
	}

	// Score.
	if got.Game.HomeScore != 38 || got.Game.AwayScore != 31 {
		t.Errorf("score = %d-%d, want 38-31", got.Game.HomeScore, got.Game.AwayScore)
	}

	// All eight stat blocks must be populated, each carrying a player name.
	blocks := map[string]*Stat{
		"pass_home_stats": got.PassHomeStats,
		"pass_away_stats": got.PassAwayStats,
		"rush_home_stats": got.RushHomeStats,
		"rush_away_stats": got.RushAwayStats,
		"rec_home_stats":  got.RecHomeStats,
		"rec_away_stats":  got.RecAwayStats,
		"def_home_stats":  got.DefHomeStats,
		"def_away_stats":  got.DefAwayStats,
	}
	for name, s := range blocks {
		if s == nil {
			t.Errorf("%s: must be populated from a real recap payload", name)
			continue
		}
		if s.Player.FullName == "" {
			t.Errorf("%s.player.fullName: must be populated", name)
		}
	}
}

func TestFetchJSONReportsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := fetchJSON(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Error("a 404 must be reported as an error")
	}
}

func TestFetchJSONReportsRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := fetchJSON(context.Background(), srv.Client(), srv.URL)
	if err == nil {
		t.Fatal("a 429 must be reported as an error")
	}
}

// TestFetchJSONReportsContextDeadlineExceeded covers the third trigger for
// gochrome's fallback path alongside 404 and 429: a slow response. Since
// V5-18 the HTTP client is injected and fetchJSON no longer takes a
// `timeout time.Duration` parameter — the deadline now comes from the
// caller's context (or from the injected client's own timeout), so this
// exercises that path directly instead of a removed parameter.
func TestFetchJSONReportsContextDeadlineExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := fetchJSON(ctx, srv.Client(), srv.URL); err == nil {
		t.Error("a context deadline exceeded while fetching must be reported as an error")
	}
}
