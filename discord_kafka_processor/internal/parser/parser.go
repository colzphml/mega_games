package parser

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var (
	gameNumberRe = regexp.MustCompile(`(?i)game\s*([0-9]+)`) // e.g. Game 12
	gameURLRe    = regexp.MustCompile(`(?i)MEGA/games/(\d+)`)
	// weekAfterKeywordRe anchors on the word "Week" so a title like
	// "Week 3 of 17" yields 3. The previous unanchored `\d+` scan kept
	// the last number found anywhere in the title.
	weekAfterKeywordRe = regexp.MustCompile(`(?i)week\s+(\d+)`)
)

type WeekUpdate struct {
	Season string `json:"season"`
	Week   int    `json:"week"`
	Title  string `json:"title,omitempty"`
}

type Result struct {
	Ready bool
	Week  *WeekUpdate
	Games []string
}

func ParseMessage(message *discordgo.Message) Result {
	if message == nil {
		return Result{}
	}
	if len(message.Embeds) == 0 {
		return Result{Ready: false}
	}

	gameSet := map[string]struct{}{}
	var week *WeekUpdate

	for _, embed := range message.Embeds {
		if embed == nil {
			continue
		}

		if week == nil && strings.Contains(embed.Title, "has advanced to") {
			if parsed, ok := parseWeek(embed.Title); ok {
				week = parsed
			}
		}

		for _, field := range embed.Fields {
			if field == nil {
				continue
			}
			if !strings.Contains(field.Value, "MEGA/games") {
				continue
			}
			for _, gameNumber := range extractGameNumbers(field.Name, field.Value) {
				gameSet[gameNumber] = struct{}{}
			}
		}

		if strings.Contains(embed.Description, "MEGA/games") {
			for _, gameNumber := range extractGameNumbers(embed.Description) {
				gameSet[gameNumber] = struct{}{}
			}
		}
	}

	games := make([]string, 0, len(gameSet))
	for game := range gameSet {
		games = append(games, game)
	}
	sort.Strings(games)

	return Result{
		Ready: week != nil || len(games) > 0,
		Week:  week,
		Games: games,
	}
}

func parseWeek(title string) (*WeekUpdate, bool) {
	season := ""
	if strings.Contains(title, "Regular Season") {
		season = "regular"
	} else if strings.Contains(title, "Pre Season") {
		season = "preseason"
	} else if strings.Contains(title, "Post Season") {
		season = "postseason"
	} else {
		return nil, false
	}

	week := 0
	if m := weekAfterKeywordRe.FindStringSubmatch(title); len(m) == 2 {
		if parsed, err := strconv.Atoi(m[1]); err == nil {
			week = parsed
		}
	}

	return &WeekUpdate{Season: season, Week: week, Title: strings.TrimSpace(title)}, true
}

func extractGameNumbers(values ...string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value == "" {
			continue
		}
		for _, match := range gameNumberRe.FindAllStringSubmatch(value, -1) {
			if len(match) == 2 {
				set[match[1]] = struct{}{}
			}
		}
		for _, match := range gameURLRe.FindAllStringSubmatch(value, -1) {
			if len(match) == 2 {
				set[match[1]] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	return result
}
