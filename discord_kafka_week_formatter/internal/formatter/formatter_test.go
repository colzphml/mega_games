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
	if !strings.Contains(got, "Игра плейофф, расписание см. в игре") {
		t.Errorf("postseason post must explain why there is no match list, got:\n%s", got)
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
	if !strings.Contains(got, "TB(CPU) @ [ATL](t.me/alice)") {
		t.Errorf("regular season post must list games, got:\n%s", got)
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
