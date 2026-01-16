package formatter

import (
	"fmt"
	"strings"
	"time"

	"github.com/colzphml/mega_games/discord_kafka_week_formatter/internal/store"
)

func BuildWeekMessage(payload store.WeekPayload, teams map[string]store.Team, games []store.Game) (string, error) {
	if payload.Season == "" || payload.Week == 0 {
		return "", fmt.Errorf("invalid week payload")
	}

	title := fmt.Sprintf("MEGA has advanced to %s Week %d", seasonTitle(payload.Season), payload.Week)
	var textBuilder strings.Builder
	textBuilder.WriteString(fmt.Sprintf("*%s*\n", title))

	for _, game := range games {
		away, awayOK := teams[game.Away]
		home, homeOK := teams[game.Home]
		if !awayOK || !homeOK {
			return "", fmt.Errorf("missing team data for %s @ %s", game.Away, game.Home)
		}

		awayCPU := isCPU(away.Player)
		homeCPU := isCPU(home.Player)

		var gameInfo string
		switch {
		case awayCPU && homeCPU:
			gameInfo = fmt.Sprintf("\n%s(CPU) @ %s(CPU)", away.ShortName, home.ShortName)
		case awayCPU:
			gameInfo = fmt.Sprintf("\n%s(CPU) @ [%s](%s)", away.ShortName, home.ShortName, telegramLink(home.Player))
		case homeCPU:
			gameInfo = fmt.Sprintf("\n[%s](%s) @ %s(CPU)", away.ShortName, telegramLink(away.Player), home.ShortName)
		default:
			gameInfo = fmt.Sprintf("\n[%s](%s) @ [%s](%s)", away.ShortName, telegramLink(away.Player), home.ShortName, telegramLink(home.Player))
		}

		textBuilder.WriteString(gameInfo)
	}

	base := textBuilder.String()
	deadline := time.Now().In(time.Local).Add(40 * time.Hour).Format("02 Jan 2006 15:04")
	template := fmt.Sprintf("%s\n\n_❗️Пожалуйста, договоритесь прямо сейчас о матче во избежание затяжек шага.\n\nДо %s просьба указать анонс матча реплаем к этому посту_", base, deadline)
	return template, nil
}

func seasonTitle(season string) string {
	if season == "preseason" {
		return "Pre Season"
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
