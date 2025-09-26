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

	// cfg := &config.Config{}
	// err := cfg.LoadConfig("config.yaml")
	// if err != nil {
	// 	log.Error().Err(err).Msg("failed to load config")
	// }
	// ctx := context.Background()
	// messagesChan := make(chan model.DiscordGame)
	// targetChan := make(chan model.TargetMessage)

	// client, err := selenium.NewClient(ctx, cfg, messagesChan, targetChan)
	// if err != nil {
	// 	log.Error().Err(err).Msg("Failed to create client")
	// }
	// err = client.ProceedUrl(ctx, "https://neonsportz.com/leagues/MEGA/games/5391690")
	// if err != nil {
	// 	log.Error().Err(err).Msg("Failed to proceed url")
	// }
	app.BuildApp()
}
