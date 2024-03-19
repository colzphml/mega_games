package app

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/colzphml/mega_games/internal/middle"
	source "github.com/colzphml/mega_games/internal/sourceSM"
	target "github.com/colzphml/mega_games/internal/targetSM"
	"github.com/colzphml/mega_games/pkg/config"
	"github.com/rs/zerolog"
)

var (
	buildVersion string = "N/A"
	buildDate    string = "N/A"
	buildCommit  string = "N/A"
	log                 = zerolog.New(os.Stdout).With().Str("package", "app").Timestamp().Logger()
)

func loadConfig(configFile string) (*config.Config, error) {
	cfg := &config.Config{}
	err := cfg.LoadConfig(configFile)
	if err != nil {
		log.Error().Err(err).Msg("failed to load config")
		return nil, err
	}
	log.Info().Msg("config loaded successfully")
	return cfg, nil
}

func setupSignalHandler(cancel context.CancelFunc) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	go func() {
		<-sigChan
		cancel()
	}()
}

func BuildApp() {
	log.Info().Str("version", buildVersion).Str("date", buildDate).Str("commit", buildCommit).Msg("bot started")

	cfg, err := loadConfig("config.yaml")
	if err != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	setupSignalHandler(cancel)

	sourceSM, err := source.NewSource(ctx, cfg)
	if err != nil {
		log.Error().Err(err).Msg("failed to create source")
		return
	}

	middleService, err := middle.NewMiddler(ctx, cfg)
	if err != nil {
		log.Error().Err(err).Msg("failed to create middle")
		return
	}

	targetSM, err := target.NewSource(ctx, cfg)
	if err != nil {
		log.Error().Err(err).Msg("failed to create target")
		return
	}

	messagesChan := make(chan string)
	wg := &sync.WaitGroup{}

	wg.Add(1)
	go sourceSM.ReadMessages(ctx, wg, messagesChan)

	wg.Add(1)
	go middleService.ReadMessages(ctx, wg, messagesChan)

	wg.Add(1)
	go targetSM.ProceedFiles(ctx, wg)

	<-ctx.Done() // Wait for the context to be cancelled

	sourceSM.Close()
	log.Info().Msg("Source client stopped")

	middleService.Close()
	log.Info().Msg("Middle client stopped")

	targetSM.Close()
	log.Info().Msg("Target client stopped")

	wg.Wait() // Ensure all goroutines have finished
	log.Info().Msg("Application stopped gracefully")
}
