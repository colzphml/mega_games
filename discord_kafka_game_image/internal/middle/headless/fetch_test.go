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
// tests in this file. It is a real, anonymised API response (Patriots 31,
// Commanders 38), not a synthetic literal: NeonSportz mixes JSON numbers
// and JSON strings for numeric fields inconsistently across stat blocks,
// and only a fixture pinned from a live response can catch a regression in
// that mix.
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
// exists. In the recorded response, defSacks arrives as the string "0.00"
// in both defensive blocks, while defTotalTackles, a few fields away in
// the same object, arrives as the plain number — the same API,
// inconsistent even within one block, and inconsistent independently in
// both the home and away block. A plain `int` field would fail to decode
// this payload at all ("json: cannot unmarshal string into Go struct
// field ... of type int"); JSONInt must accept both forms.
func TestJSONIntParsesRealFixtureStringField(t *testing.T) {
	var rec Recap
	if err := json.Unmarshal(readFixture(t), &rec); err != nil {
		t.Fatalf("unmarshal real recap fixture: %v", err)
	}

	if rec.DefHomeStats == nil {
		t.Fatal("def_home_stats must be populated from the fixture")
	}
	if got := int(rec.DefHomeStats.DefSacks); got != 0 {
		t.Errorf("def_home_stats.DefSacks (string field %q in the fixture) = %d, want 0", "0.00", got)
	}
	if got := int(rec.DefHomeStats.DefTotalTackles); got != 3 {
		t.Errorf("def_home_stats.DefTotalTackles (numeric field in the same block) = %d, want 3", got)
	}

	// def_away_stats carries the same string-vs-number split independently
	// of def_home_stats. Both must decode correctly on their own — a fix
	// that only special-cased the home block would still pass a
	// home-only check.
	if rec.DefAwayStats == nil {
		t.Fatal("def_away_stats must be populated from the fixture")
	}
	if got := int(rec.DefAwayStats.DefSacks); got != 0 {
		t.Errorf("def_away_stats.DefSacks (string field %q in the fixture) = %d, want 0", "0.00", got)
	}
	if got := int(rec.DefAwayStats.DefTotalTackles); got != 1 {
		t.Errorf("def_away_stats.DefTotalTackles (numeric field in the same block) = %d, want 1", got)
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

	t.Run("teams", func(t *testing.T) {
		if got.Game.HomeTeam.DisplayName != "Commanders" {
			t.Errorf("home team = %q, want %q", got.Game.HomeTeam.DisplayName, "Commanders")
		}
		if got.Game.AwayTeam.DisplayName != "Patriots" {
			t.Errorf("away team = %q, want %q", got.Game.AwayTeam.DisplayName, "Patriots")
		}
	})

	t.Run("score", func(t *testing.T) {
		if got.Game.HomeScore != 38 || got.Game.AwayScore != 31 {
			t.Errorf("score = %d-%d, want 38-31", got.Game.HomeScore, got.Game.AwayScore)
		}
	})

	// statField pins one decoded numeric value against the fixture.
	type statField struct {
		label string
		got   JSONInt
		want  int
	}

	// Each of the eight stat blocks is checked against its own player name
	// plus a couple of real, nonzero numeric values pulled straight from
	// the fixture. Checking exact values — not just "non-nil, non-empty
	// name" — is what catches a mistyped JSON tag or a field swapped
	// during a refactor: under a presence-only check, a field that
	// silently decodes to its zero value, or a home/away block wired to
	// the wrong JSON key, would still pass. Values are deliberately
	// nonzero so a field silently zeroing out cannot hide as "correctly
	// parsed zero".
	//
	// The `fields` closure is only invoked after the nil check below, so
	// a block missing from the payload reports one clear failure instead
	// of a nil-pointer panic.
	blocks := []struct {
		name       string
		stat       *Stat
		wantPlayer string
		fields     func(s *Stat) []statField
	}{
		{"pass_home_stats", got.PassHomeStats, "Jayden Daniels", func(s *Stat) []statField {
			return []statField{{"PassYds", s.PassYds, 267}, {"PassTDs", s.PassTDs, 3}}
		}},
		{"pass_away_stats", got.PassAwayStats, "Drake Maye", func(s *Stat) []statField {
			return []statField{{"PassComp", s.PassComp, 22}, {"PassYds", s.PassYds, 226}}
		}},
		{"rush_home_stats", got.RushHomeStats, "Jonathan Taylor", func(s *Stat) []statField {
			return []statField{{"RushYds", s.RushYds, 104}, {"RushTDs", s.RushTDs, 2}}
		}},
		{"rush_away_stats", got.RushAwayStats, "Jahlil Howard", func(s *Stat) []statField {
			return []statField{{"RushAtt", s.RushAtt, 18}, {"RushYds", s.RushYds, 145}}
		}},
		{"rec_home_stats", got.RecHomeStats, "Ben Sinnott", func(s *Stat) []statField {
			return []statField{{"RecYds", s.RecYds, 88}, {"RecTDs", s.RecTDs, 2}}
		}},
		{"rec_away_stats", got.RecAwayStats, "Mitch McKee", func(s *Stat) []statField {
			return []statField{{"RecCatches", s.RecCatches, 6}, {"RecYds", s.RecYds, 81}}
		}},
		{"def_home_stats", got.DefHomeStats, "Danny Stutsman", func(s *Stat) []statField {
			return []statField{{"DefTotalTackles", s.DefTotalTackles, 3}, {"DefInts", s.DefInts, 1}}
		}},
		{"def_away_stats", got.DefAwayStats, "Robert Spillane", func(s *Stat) []statField {
			return []statField{{"DefTotalTackles", s.DefTotalTackles, 1}, {"DefTDs", s.DefTDs, 1}}
		}},
	}

	for _, b := range blocks {
		t.Run(b.name, func(t *testing.T) {
			if b.stat == nil {
				t.Fatalf("%s: must be populated from a real recap payload", b.name)
			}
			if b.stat.Player.FullName != b.wantPlayer {
				t.Errorf("%s.player.fullName = %q, want %q", b.name, b.stat.Player.FullName, b.wantPlayer)
			}
			for _, f := range b.fields(b.stat) {
				if int(f.got) != f.want {
					t.Errorf("%s.%s = %d, want %d", b.name, f.label, int(f.got), f.want)
				}
			}
		})
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
// exercises the caller-context half of that directly instead of a removed
// parameter. See TestFetchJSONReportsClientTimeout for the other half —
// the *http.Client.Timeout mechanism actually used in production.
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

// TestFetchJSONReportsClientTimeout covers the timeout mechanism actually
// wired up in production: NewClient (headless.go) builds the injected doer
// as &http.Client{Timeout: cfg.FetchTimeout} — a whole-round-trip client
// timeout, not a context deadline set by the caller of Fetch. A passing
// context-deadline test alone would not catch a regression in that
// client-level wiring, since fetchJSON never sets a client timeout itself.
func TestFetchJSONReportsClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 20 * time.Millisecond}

	if _, err := fetchJSON(context.Background(), client, srv.URL); err == nil {
		t.Error("an http.Client timeout must be reported as an error")
	}
}
