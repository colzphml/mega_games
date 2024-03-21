package main

import (
	"os"

	"github.com/colzphml/mega_games/internal/app"
	"github.com/rs/zerolog"
)

var log = zerolog.New(os.Stdout).With().Str("package", "main").Timestamp().Logger()

func main() {
	// teams, err := utils.ParseCSVFileToTeams("../MEGA_teams.csv")
	// if err != nil {
	// 	log.Error().Err(err).Msg("Failed to parse teams from CSV")
	// 	os.Exit(1)
	// }
	// log.Info().Msgf("Teams: %v", teams)
	app.BuildApp()
}
