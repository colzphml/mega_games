package main

import (
	"os"

	"github.com/colzphml/mega_games/internal/app"
	"github.com/rs/zerolog"
)

var log = zerolog.New(os.Stdout).With().Str("package", "main").Timestamp().Logger()

func main() {
	app.BuildApp()
}
