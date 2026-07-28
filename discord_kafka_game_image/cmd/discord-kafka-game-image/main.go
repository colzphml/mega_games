package main

import (
	"context"
	"errors"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	kafkago "github.com/segmentio/kafka-go"

	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/kafka"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/middle"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/pgstore"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/processor"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/storage"
	"github.com/colzphml/mega_games/internal/common/health"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	log := zerolog.New(os.Stdout).With().Str("service", "discord-kafka-game-image").Timestamp().Logger()

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

	pgStore, err := pgstore.New(ctx, cfg.PostgresDSN(), log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to postgres")
	}
	defer pgStore.Close()

	if err := health.WaitFor(ctx, "postgres", cfg.PostgresMaxAttempts, cfg.PostgresRetryDelay, log, pgStore.Ping); err != nil {
		log.Fatal().Err(err).Msg("postgres not ready")
	}
	if err := pgStore.EnsureSchema(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to ensure postgres schema")
	}

	var objectStore *storage.Client
	buildObjectStore := func(waitCtx context.Context) error {
		client, err := storage.New(waitCtx, cfg, log)
		if err != nil {
			return err
		}
		objectStore = client
		return nil
	}
	if err := health.WaitFor(ctx, "minio", cfg.MinioMaxAttempts, cfg.MinioRetryDelay, log, buildObjectStore); err != nil {
		log.Fatal().Err(err).Msg("minio not ready")
	}

	producer, err := kafka.NewProducer(cfg.KafkaBrokers, cfg.KafkaImageTopic, cfg.KafkaClientID, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create kafka producer")
	}
	defer producer.Close()

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        cfg.KafkaBrokers,
		Topic:          cfg.KafkaGameTopic,
		GroupID:        cfg.KafkaConsumerGroup,
		MinBytes:       1,
		MaxBytes:       10e6,
		MaxWait:        cfg.KafkaReadTimeout,
		CommitInterval: 0,
	})
	defer reader.Close()

	fetcher, err := middle.NewFetcher(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create fetcher")
	}
	defer func() {
		if err := fetcher.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close fetcher")
		}
	}()

	healthSrv := health.NewServer(cfg.HealthAddr, log)
	healthSrv.AddLiveness("postgres", pgStore.Ping)
	healthSrv.AddLiveness("minio", objectStore.Ping)
	healthSrv.AddLiveness("kafka", func(ctx context.Context) error {
		if len(cfg.KafkaBrokers) == 0 {
			return errors.New("no kafka brokers configured")
		}
		dialer := &net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", cfg.KafkaBrokers[0])
		if err != nil {
			return err
		}
		return conn.Close()
	})
	healthSrv.Start(ctx)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = healthSrv.Shutdown(shutdownCtx)
	}()

	proc := processor.New(cfg, pgStore, reader, producer, fetcher, objectStore, log)
	log.Info().Str("topic", cfg.KafkaGameTopic).Str("group", cfg.KafkaConsumerGroup).Msg("game image consumer started")
	if err := proc.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("processor stopped with error")
	}
}
