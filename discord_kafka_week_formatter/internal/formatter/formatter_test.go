package formatter

import (
	"strings"
	"testing"

	"github.com/colzphml/mega_games/discord_kafka_week_formatter/internal/store"
)

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
