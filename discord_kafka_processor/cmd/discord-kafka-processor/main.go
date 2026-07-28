package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	kafkago "github.com/segmentio/kafka-go"

	"github.com/colzphml/mega_games/discord_kafka_processor/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_processor/internal/discord"
	"github.com/colzphml/mega_games/discord_kafka_processor/internal/kafka"
	"github.com/colzphml/mega_games/discord_kafka_processor/internal/processor"
	"github.com/colzphml/mega_games/discord_kafka_processor/internal/store"
	"github.com/colzphml/mega_games/internal/common/health"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	log := zerolog.New(os.Stdout).With().Str("service", "discord-kafka-processor").Timestamp().Logger()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	if location, err := time.LoadLocation(cfg.Timezone); err != nil {
		log.Error().Err(err).Str("timezone", cfg.Timezone).Msg("failed to load timezone")
	} else {
		time.Local = location
	}

	log.Info().
		Str("version", buildVersion).
		Str("commit", buildCommit).
		Str("build_date", buildDate).
		Str("timezone", cfg.Timezone).
		Str("tz_offset", time.Now().Format("-0700")).
		Msg("build info")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	storeClient, err := store.New(ctx, cfg.PostgresDSN(), log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to postgres")
	}
	defer storeClient.Close()

	if err := health.WaitFor(ctx, "postgres", cfg.PostgresMaxAttempts, cfg.PostgresRetryDelay, log, storeClient.Ping); err != nil {
		log.Fatal().Err(err).Msg("postgres not ready")
	}
	if err := storeClient.EnsureSchema(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to ensure postgres schema")
	}

	discordClient, err := discord.NewClient(cfg.DiscordToken, cfg.DiscordChannelID, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create discord client")
	}
	defer func() {
		_ = discordClient.Close()
	}()
	discordClient.StartHealth(ctx, cfg.DiscordHealthInt)

	producer, err := kafka.NewProducer(cfg.KafkaBrokers, cfg.KafkaWeekTopic, cfg.KafkaGameTopic, cfg.KafkaClientID, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create kafka producer")
	}
	defer producer.Close()

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        cfg.KafkaBrokers,
		Topic:          cfg.KafkaInputTopic,
		GroupID:        cfg.KafkaConsumerGroup,
		MinBytes:       1,
		MaxBytes:       10e6,
		MaxWait:        cfg.KafkaReadTimeout,
		CommitInterval: 0,
	})
	defer reader.Close()

	healthSrv := health.NewServer(cfg.HealthAddr, log)
	healthSrv.AddLiveness("postgres", storeClient.Ping)
	// Discord is a readiness concern only: restarting this container
	// cannot fix an outage on Discord's side, and autoheal watches
	// /health, so listing it there caused restart loops.
	healthSrv.AddReadiness("discord", func(ctx context.Context) error {
		if !discordClient.Healthy() {
			return errors.New("discord api unreachable")
		}
		return nil
	})
	healthSrv.Start(ctx)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = healthSrv.Shutdown(shutdownCtx)
	}()

	proc := processor.New(cfg, storeClient, discordClient, producer, reader, log)
	if err := proc.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("processor stopped with error")
	}
}
