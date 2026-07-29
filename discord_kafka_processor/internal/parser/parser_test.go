package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func loadFixture(t *testing.T, name string) *discordgo.Message {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var msg discordgo.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return &msg
}

// week_advance.json: a single rich embed, title "MEGA has advanced to
// Regular Season Week 15", no fields. Captured from a live week-change
// post.
func TestParseWeekAdvance(t *testing.T) {
	got := ParseMessage(loadFixture(t, "week_advance.json"))
	if !got.Ready {
		t.Fatal("a week-advance message must parse as ready")
	}
	if got.Week == nil {
		t.Fatal("week update must be extracted")
	}
	if got.Week.Season != "regular" {
		t.Errorf("season = %q, want regular", got.Week.Season)
	}
	if got.Week.Week != 15 {
		t.Errorf("week = %d, want 15", got.Week.Week)
	}
	if want := "MEGA has advanced to Regular Season Week 15"; got.Week.Title != want {
		t.Errorf("title = %q, want %q", got.Week.Title, want)
	}
	if len(got.Games) != 0 {
		t.Errorf("games = %v, want none: this message carries no game links", got.Games)
	}
}

// games_list.json: title "MEGA - Week 14 Games (New)", 9 fields, each a
// "[Away ... - ... Home](.../MEGA/games/<id>)" line, some with a "CPU"
// player marker. The title never contains "has advanced to", so this
// message must not produce a week update.
func TestParseGamesList(t *testing.T) {
	got := ParseMessage(loadFixture(t, "games_list.json"))
	if !got.Ready {
		t.Fatal("a games message must parse as ready")
	}
	if got.Week != nil {
		t.Errorf("week = %+v, want nil: the games-list title never contains \"has advanced to\"", got.Week)
	}
	if len(got.Games) == 0 {
		t.Fatal("game ids must be extracted")
	}

	seen := map[string]bool{}
	for _, g := range got.Games {
		if seen[g] {
			t.Errorf("duplicate game id %q — the parser must deduplicate", g)
		}
		seen[g] = true
	}

	// Pin the exact set actually observed: all 9 ids come from the
	// "MEGA/games/<id>" links (sort.Strings happens to match numeric
	// order here because every id is 8 digits). Every field's "name" in
	// this fixture is a lone zero-width space, not "Game N" text, so
	// gameNumberRe never contributes here — see
	// TestParseMessageDeduplicatesGameNumberFromTextAndURL below for a
	// case that exercises that regex.
	want := []string{
		"18294119", "18294120", "18294121", "18294122", "18294126",
		"18294128", "18294129", "18294130", "18294131",
	}
	if !reflect.DeepEqual(got.Games, want) {
		t.Errorf("games = %v, want %v", got.Games, want)
	}
}

// no_embeds.json: `{"embeds":[]}`. Discord populates embeds asynchronously
// after a message is posted, so a just-fetched message can legitimately
// have none yet; the parser must treat that as not ready rather than as
// "an update with no content".
func TestParseMessageWithoutEmbedsIsNotReady(t *testing.T) {
	got := ParseMessage(loadFixture(t, "no_embeds.json"))
	if got.Ready {
		t.Error("a message without embeds is not ready: Discord fills embeds asynchronously")
	}
	if got.Week != nil {
		t.Errorf("week = %+v, want nil", got.Week)
	}
	if len(got.Games) != 0 {
		t.Errorf("games = %v, want none", got.Games)
	}
}

// week_wildcard.json: title "MEGA has advanced to Post Season Wild Card" —
// no digits anywhere. parseWeek still recognises "Post Season" and
// returns ok=true with Week left at its zero value, so ParseMessage
// (which only checks `week != nil`) reports Ready=true even though the
// week number is meaningless.
//
// discord_kafka_week_formatter/internal/formatter.BuildWeekMessage
// (formatter.go:36-42) falls back to payload.Title whenever Week <= 0:
//
//	if payload.Week > 0 {
//		title = fmt.Sprintf("MEGA has advanced to %s Week %d", ...)
//	} else if payload.Title != "" {
//		title = payload.Title
//	} else {
//		return "", fmt.Errorf("invalid week payload")
//	}
//
// That fallback only works if the parser always fills Title with the
// verbatim heading, which is what this test pins.
func TestParseWeekWildcardHasNoNumberButStaysReady(t *testing.T) {
	got := ParseMessage(loadFixture(t, "week_wildcard.json"))
	if !got.Ready {
		t.Fatal("a wild-card message must still parse as ready: the season keyword matched")
	}
	if got.Week == nil {
		t.Fatal("week update must be extracted")
	}
	if got.Week.Season != "postseason" {
		t.Errorf("season = %q, want postseason", got.Week.Season)
	}
	if got.Week.Week != 0 {
		t.Errorf("week = %d, want 0: the title has no week number", got.Week.Week)
	}
	want := "MEGA has advanced to Post Season Wild Card"
	if got.Week.Title != want {
		t.Errorf("title = %q, want %q (the formatter falls back to this verbatim string whenever Week is 0)", got.Week.Title, want)
	}
	if len(got.Games) != 0 {
		t.Errorf("games = %v, want none", got.Games)
	}
}

// week_offseason.json: title "MEGA has advanced to Offseason Week 1".
// parseWeek only recognises "Regular Season", "Pre Season" and
// "Post Season" (parser.go:86-94); "Offseason" is none of those, so
// parseWeek returns ok=false, week stays nil, and — because this message
// also carries no game links — Ready is false too.
//
// ParseMessage cannot distinguish "season not recognised" from "embeds
// not populated yet": both produce an identical Result{Ready: false}.
// Downstream, discord_kafka_processor/internal/processor.fetchAndParse
// (processor.go:245-259) treats every not-ready message the same way: it
// retries fetching until the message is older than cfg.EmbedWarmup
// (isMessagePastWarmup, default 5s — true almost immediately for a
// message that is not brand new), then returns Skip=true.
// handleMessage (processor.go:181-187) turns Skip into
// store.MarkProcessed with an info-level "message skipped" log and
// nothing else — no error, no Kafka write, no distinct status. The
// message is filed as done exactly like a successfully processed one and
// will never be looked at again (store.ListPending only selects
// status=new). An offseason transition is silently and permanently
// dropped; nothing downstream is ever told the season changed.
func TestParseWeekOffseasonIsUnrecognisedAndNotReady(t *testing.T) {
	got := ParseMessage(loadFixture(t, "week_offseason.json"))
	if got.Ready {
		t.Error("an offseason message must not parse as ready: the season is not one parseWeek recognises")
	}
	if got.Week != nil {
		t.Errorf("week = %+v, want nil", got.Week)
	}
	if len(got.Games) != 0 {
		t.Errorf("games = %v, want none", got.Games)
	}
}

// Same case as above, pinned directly at the parseWeek level so a future
// change to ParseMessage's Ready computation can't hide a change in
// season recognition.
func TestParseWeekDirectlyRejectsOffseason(t *testing.T) {
	if _, ok := parseWeek("MEGA has advanced to Offseason Week 1"); ok {
		t.Error(`"Offseason" is not Regular/Pre/Post Season and must not parse`)
	}
}

// P3-3 regression. parseWeek used to scan every integer in the title with
// weekNumberRe (`\d+`, unanchored) and keep the last one seen
// (parser.go:96-101, pre-fix). None of the five captured fixtures exposes
// this: every real title seen in production has at most one number, so
// "last number" and "the week number" always agreed by coincidence. A
// title shaped like "Week 3 of 17" makes them disagree.
//
// Confirmed against the pre-fix parser (by calling parseWeek directly,
// before applying the Step 4 regex change below) that this exact input
// produced Week: 17, not 3 — so this test is a real regression pin, not
// a vacuous one.
func TestParseWeekTakesTheWeekNumberNotTheLast(t *testing.T) {
	week, ok := parseWeek("MEGA has advanced to Regular Season Week 3 of 17")
	if !ok {
		t.Fatal("title must parse")
	}
	if week.Week != 3 {
		t.Errorf("week = %d, want 3", week.Week)
	}
}

func TestParseWeekRejectsUnknownSeason(t *testing.T) {
	if _, ok := parseWeek("MEGA has advanced to Something Else Week 3"); ok {
		t.Error("an unrecognised season must not parse")
	}
}

func TestParseNilMessage(t *testing.T) {
	got := ParseMessage(nil)
	if got.Ready {
		t.Error("nil message must not be ready")
	}
	if got.Week != nil {
		t.Errorf("week = %+v, want nil", got.Week)
	}
	if len(got.Games) != 0 {
		t.Errorf("games = %v, want none", got.Games)
	}
}

// Synthetic case, not from testdata: games_list.json alone does not
// exercise cross-source deduplication. In that fixture every field
// "name" is a lone zero-width space, and the word "game" only ever
// occurs as a substring of the "MEGA/games/<id>" URL, where gameNumberRe
// (`(?i)game\s*([0-9]+)`) cannot match — the character right after
// "game" there is "s", not whitespace or a digit. So on real captured
// data, gameNumberRe never fires and every id comes from gameURLRe
// alone: nothing is ever a candidate for a same-message, two-source
// collision. This builds a message by hand where a field's Name (e.g.
// "Game 18294129", the way Discord embed field names sometimes do carry
// text) and its Value's URL both resolve to the same id, and checks that
// they collapse into a single entry rather than two.
func TestParseMessageDeduplicatesGameNumberFromTextAndURL(t *testing.T) {
	msg := &discordgo.Message{
		Embeds: []*discordgo.MessageEmbed{
			{
				Title: "MEGA - Week 14 Games (New)",
				Fields: []*discordgo.MessageEmbedField{
					{
						Name:  "Game 18294129",
						Value: "[49ers @ Rams](https://neonsportz.com/leagues/MEGA/games/18294129)",
					},
				},
			},
		},
	}

	got := ParseMessage(msg)
	want := []string{"18294129"}
	if !reflect.DeepEqual(got.Games, want) {
		t.Errorf("games = %v, want %v (the same id from the field's Name text and from its Value's URL must collapse to one entry)", got.Games, want)
	}
}
