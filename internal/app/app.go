package app

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

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

func BuildApp() {
	log.Info().Str("version", buildVersion).Str("date", buildDate).Str("commit", buildCommit).Msg("scheduler started")
	cfg := &config.Config{}
	err := cfg.LoadConfig("config.yaml")
	if err != nil {
		log.Error().Err(err).Msg("failed to load config")
		return
	}
	log.Info().Msg("config loaded")

	ctx, cancel := context.WithCancel(context.Background())

	sourceSM, err := source.NewSource(ctx, cfg)
	if err != nil {
		log.Error().Err(err).Msg("failed to create source")
		cancel()
		return
	}

	targetSM, err := target.NewSource(ctx, cfg)
	if err != nil {
		log.Error().Err(err).Msg("failed to create target")
		cancel()
		return
	}

	wg := &sync.WaitGroup{}
	// jsch := scheduler.NewJobScheduler(ctx, cfg)
	// // create database connection
	// db, err := database.NewDatabase(ctx, cfg)
	// if err != nil {
	// 	log.Error().Err(err).Msg("failed to create database")
	// 	cancel()
	// 	return
	// }
	// jsch.DB = db
	// // create k8s client
	// k8sClient, err := k8sclient.NewK8sclient(ctx, cfg)
	// if err != nil {
	// 	log.Error().Err(err).Msg("failed to create k8s client")
	// 	cancel()
	// 	return
	// }
	// jsch.K8sClient = k8sClient
	// // create wait group
	// wg := &sync.WaitGroup{}
	// jsch.WG = wg

	// go jsch.RunWorker(ctx)
	// go jsch.ScheduleWorker(ctx)
	// go jsch.UpdateScheduleWorker(ctx)
	// go jsch.CheckerWorker(ctx)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	<-sigChan
	cancel()
	sourceSM.Close()
	targetSM.Close()
	wg.Wait()
	log.Info().Msg("bot stopped")
}
