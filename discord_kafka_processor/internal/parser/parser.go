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
)

type WeekUpdate struct {
	Season string `json:"season"`
	Week   int    `json:"week"`
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

	ready := false
	gameSet := map[string]struct{}{}
	var week *WeekUpdate

	for _, embed := range message.Embeds {
		if embed == nil {
			continue
		}
		hasContent := strings.TrimSpace(embed.Title) != "" || strings.TrimSpace(embed.Description) != "" || len(embed.Fields) > 0
		if hasContent {
			ready = true
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
			if gameNumber, ok := extractGameNumber(field.Name, field.Value); ok {
				gameSet[gameNumber] = struct{}{}
			}
		}

		if strings.Contains(embed.Description, "MEGA/games") {
			if gameNumber, ok := extractGameNumber(embed.Description); ok {
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
		Ready: ready,
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
	} else {
		return nil, false
	}

	words := strings.Fields(title)
	if len(words) == 0 {
		return nil, false
	}
	week, err := strconv.Atoi(words[len(words)-1])
	if err != nil {
		return nil, false
	}

	return &WeekUpdate{Season: season, Week: week}, true
}

func extractGameNumber(values ...string) (string, bool) {
	for _, value := range values {
		if value == "" {
			continue
		}
		if match := gameNumberRe.FindStringSubmatch(value); len(match) == 2 {
			return match[1], true
		}
		if match := gameURLRe.FindStringSubmatch(value); len(match) == 2 {
			return match[1], true
		}
	}
	return "", false
}
