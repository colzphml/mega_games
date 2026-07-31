package formatter

import (
	"strings"
	"testing"
	"time"

	"github.com/colzphml/mega_games/discord_kafka_week_formatter/internal/store"
)

// testDeadline mirrors the WEEK_ANNOUNCE_DEADLINE default; the exact value
// doesn't matter to these tests since none of them assert the rendered date.
const testDeadline = 40 * time.Hour

func testTeams() map[string]store.Team {
	return map[string]store.Team{
		"Falcons":    {Name: "Falcons", ShortName: "ATL", Player: "alice"},
		"Buccaneers": {Name: "Buccaneers", ShortName: "TB", Player: "CPU"},
	}
}

func TestPostseasonExplainsMissingSchedule(t *testing.T) {
	got, err := BuildWeekMessage(
		store.WeekPayload{Season: "postseason", Week: 1},
		testTeams(),
		nil,
		testDeadline,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `Игра плейофф, расписание см\. в игре`) {
		t.Errorf("postseason post must explain why there is no match list, "+
			"with its period escaped for MarkdownV2, got:\n%s", got)
	}
	if !strings.Contains(got, "Post Season Week 1") {
		t.Errorf("postseason post must keep the heading, got:\n%s", got)
	}
	if !strings.Contains(got, "договоритесь") {
		t.Errorf("postseason post must keep the deadline footer, got:\n%s", got)
	}
}

func TestRegularSeasonListsGames(t *testing.T) {
	got, err := BuildWeekMessage(
		store.WeekPayload{Season: "regular", Week: 3},
		testTeams(),
		[]store.Game{{Home: "Falcons", Away: "Buccaneers"}},
		testDeadline,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `TB\(CPU\) @ [ATL](t.me/alice)`) {
		t.Errorf("regular season post must list games, with the literal (CPU) marker "+
			"escaped for MarkdownV2, got:\n%s", got)
	}
	if strings.Contains(got, "плейофф") {
		t.Errorf("regular season post must not mention playoffs, got:\n%s", got)
	}
}

func TestPreseasonWeekFourHasNoGames(t *testing.T) {
	got, err := BuildWeekMessage(
		store.WeekPayload{Season: "preseason", Week: 4},
		testTeams(),
		nil,
		testDeadline,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "Pre Season Week 4") {
		t.Errorf("got:\n%s", got)
	}
}

func TestGameLineEscapesNickname(t *testing.T) {
	teams := map[string]store.Team{
		"A": {Name: "A", ShortName: "A_TEAM", Player: "some_user"},
		"B": {Name: "B", ShortName: "B", Player: "CPU"},
	}
	got, err := BuildWeekMessage(
		store.WeekPayload{Season: "regular", Week: 1},
		teams,
		[]store.Game{{Home: "A", Away: "B"}},
		testDeadline,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `A\_TEAM`) {
		t.Errorf("team name must be escaped, got:\n%s", got)
	}
	if !strings.Contains(got, "t.me/some_user") {
		t.Errorf("URL must stay raw, got:\n%s", got)
	}
}

func TestGameLineEscapesDotAndDash(t *testing.T) {
	// MarkdownV2 (unlike the legacy Markdown mode) reserves '.' and '-' in
	// plain text too, not just '_' and '*'. "St.Louis" and "Bucs-Falcons"
	// are exactly the kind of real short names that would trip this.
	teams := map[string]store.Team{
		"A": {Name: "A", ShortName: "St.Louis", Player: "alice"},
		"B": {Name: "B", ShortName: "Bucs-Falcons", Player: "bob"},
	}
	got, err := BuildWeekMessage(
		store.WeekPayload{Season: "regular", Week: 1},
		teams,
		[]store.Game{{Home: "A", Away: "B"}},
		testDeadline,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `St\.Louis`) {
		t.Errorf("dot in team name must be escaped for MarkdownV2, got:\n%s", got)
	}
	if !strings.Contains(got, `Bucs\-Falcons`) {
		t.Errorf("dash in team name must be escaped for MarkdownV2, got:\n%s", got)
	}
}

func TestFooterEscapesPeriod(t *testing.T) {
	// MarkdownV2 requires '.' to be escaped everywhere in plain text,
	// including the fixed Russian footer and the formatted deadline date.
	got, err := BuildWeekMessage(
		store.WeekPayload{Season: "regular", Week: 1},
		testTeams(),
		nil,
		testDeadline,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `шага\.`) {
		t.Errorf("footer period must be escaped for MarkdownV2, got:\n%s", got)
	}
}

func TestMarkupSurvivesEscaping(t *testing.T) {
	// Escaping must only touch substituted content, never the markup
	// characters the formatter itself adds: the bold title markers and
	// the [text](url) link syntax must remain intact and unescaped.
	got, err := BuildWeekMessage(
		store.WeekPayload{Season: "regular", Week: 3},
		testTeams(),
		[]store.Game{{Home: "Falcons", Away: "Buccaneers"}},
		testDeadline,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "*MEGA has advanced to Regular Season Week 3*") {
		t.Errorf("title must stay wrapped in unescaped bold markers, got:\n%s", got)
	}
	if !strings.Contains(got, "[ATL](t.me/alice)") {
		t.Errorf("link markup must stay intact and clickable, got:\n%s", got)
	}
}

// deadlineCandidates returns the footer date text BuildWeekMessage may
// legitimately have produced for the given deadline. BuildWeekMessage
// computes its own footer from an internal time.Now() call, and
// formatter.go has no seam to pin that "now" from a test (unlike the
// pgstore/store packages' *At helpers). The footer is formatted without
// seconds, so pinning a single expected string computed from a second,
// independent time.Now() call races a minute boundary landing between the
// two calls -- rare, but real: on the correct code, a test run unlucky
// enough to straddle that boundary fails for a reason that has nothing to
// do with whether the deadline argument was honoured.
//
// Bracketing "now" with a call from immediately before and immediately
// after BuildWeekMessage runs removes that race: the internal call is
// guaranteed (Go's monotonic clock reading) to land between the two, so
// the minute-truncated footer must match the text derived from one end of
// the bracket or the other.
func deadlineCandidates(before, after time.Time, deadline time.Duration) []string {
	const layout = "02 Jan 2006 15:04"
	start := before.In(time.Local).Add(deadline).Format(layout)
	end := after.In(time.Local).Add(deadline).Format(layout)
	if start == end {
		return []string{start}
	}
	return []string{start, end}
}

func containsAny(s string, candidates []string) bool {
	for _, c := range candidates {
		if strings.Contains(s, c) {
			return true
		}
	}
	return false
}

func TestDeadlineUsesConfiguredDuration(t *testing.T) {
	// The footer date must move with the deadline argument, not a fixed
	// 40h baked into the package — that's the whole point of V5-27.
	beforeShort := time.Now()
	short, err := BuildWeekMessage(
		store.WeekPayload{Season: "regular", Week: 1},
		testTeams(),
		nil,
		1*time.Hour,
	)
	afterShort := time.Now()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	beforeLong := time.Now()
	long, err := BuildWeekMessage(
		store.WeekPayload{Season: "regular", Week: 1},
		testTeams(),
		nil,
		200*time.Hour,
	)
	afterLong := time.Now()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	shortCandidates := deadlineCandidates(beforeShort, afterShort, 1*time.Hour)
	longCandidates := deadlineCandidates(beforeLong, afterLong, 200*time.Hour)
	if !containsAny(short, shortCandidates) {
		t.Errorf("expected footer to use the 1h deadline (one of %q), got:\n%s", shortCandidates, short)
	}
	if !containsAny(long, longCandidates) {
		t.Errorf("expected footer to use the 200h deadline (one of %q), got:\n%s", longCandidates, long)
	}
	if short == long {
		t.Errorf("messages built with different deadlines must not be identical")
	}
}
