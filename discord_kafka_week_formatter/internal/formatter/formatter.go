package formatter

import (
	"fmt"
	"strings"
	"time"

	"github.com/colzphml/mega_games/discord_kafka_week_formatter/internal/store"
)

// announceDeadline is how long players have to announce their match after
// the week-change post goes out. In V5-27 this becomes configurable.
var announceDeadline = 40 * time.Hour

// postseasonNote replaces the match list for playoff weeks.
//
// There is no source for a playoff schedule: the regular season is
// exported once into schedule_games, and the playoff bracket is never
// exported at all. An empty list would look like a bug; this says so
// plainly.
const postseasonNote = "Игра плейофф, расписание см. в игре"

func BuildWeekMessage(payload store.WeekPayload, teams map[string]store.Team, games []store.Game) (string, error) {
	if payload.Season == "" {
		return "", fmt.Errorf("invalid week payload")
	}

	title := ""
	if payload.Week > 0 {
		title = fmt.Sprintf("MEGA has advanced to %s Week %d", seasonTitle(payload.Season), payload.Week)
	} else if payload.Title != "" {
		title = payload.Title
	} else {
		return "", fmt.Errorf("invalid week payload")
	}

	var textBuilder strings.Builder
	textBuilder.WriteString(fmt.Sprintf("*%s*\n", title))

	if payload.Season == "postseason" {
		textBuilder.WriteString("\n" + postseasonNote + "\n")
	} else {
		for _, game := range games {
			away, awayOK := teams[game.Away]
			home, homeOK := teams[game.Home]
			if !awayOK || !homeOK {
				return "", fmt.Errorf("missing team data for %s @ %s", game.Away, game.Home)
			}
			textBuilder.WriteString(formatGameLine(away, home))
		}
	}

	base := textBuilder.String()
	deadline := time.Now().In(time.Local).Add(announceDeadline).Format("02 Jan 2006 15:04")
	return fmt.Sprintf("%s\n\n_❗️Пожалуйста, договоритесь прямо сейчас о матче во избежание затяжек шага.\n\nДо %s просьба указать анонс матча реплаем к этому посту_", base, deadline), nil
}

// formatGameLine renders one "away @ home" line with CPU markers and
// Telegram mentions for human players. The exact text goes out to people in
// Telegram, so the format must not change.
func formatGameLine(away, home store.Team) string {
	awayCPU := isCPU(away.Player)
	homeCPU := isCPU(home.Player)
	switch {
	case awayCPU && homeCPU:
		return fmt.Sprintf("\n%s(CPU) @ %s(CPU)", away.ShortName, home.ShortName)
	case awayCPU:
		return fmt.Sprintf("\n%s(CPU) @ [%s](%s)", away.ShortName, home.ShortName, telegramLink(home.Player))
	case homeCPU:
		return fmt.Sprintf("\n[%s](%s) @ %s(CPU)", away.ShortName, telegramLink(away.Player), home.ShortName)
	default:
		return fmt.Sprintf("\n[%s](%s) @ [%s](%s)", away.ShortName, telegramLink(away.Player), home.ShortName, telegramLink(home.Player))
	}
}

func seasonTitle(season string) string {
	if season == "preseason" {
		return "Pre Season"
	}
	if season == "postseason" {
		return "Post Season"
	}
	return "Regular Season"
}

func telegramLink(player string) string {
	player = strings.TrimSpace(player)
	player = strings.TrimPrefix(player, "@")
	if player == "" || strings.EqualFold(player, "CPU") {
		return ""
	}
	return "t.me/" + player
}

func isCPU(player string) bool {
	player = strings.TrimSpace(player)
	return player == "" || strings.EqualFold(player, "CPU")
}
