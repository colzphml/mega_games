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

	"github.com/colzphml/mega_games/discord_kafka_telegram_game_sender/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_telegram_game_sender/internal/processor"
	"github.com/colzphml/mega_games/discord_kafka_telegram_game_sender/internal/storage"
	"github.com/colzphml/mega_games/discord_kafka_telegram_game_sender/internal/store"
	"github.com/colzphml/mega_games/discord_kafka_telegram_game_sender/internal/telegram"
	"github.com/colzphml/mega_games/internal/common/health"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	log := zerolog.New(os.Stdout).With().Str("service", "discord-kafka-telegram-game-sender").Timestamp().Logger()

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

	telegramClient, err := telegram.New(cfg.TelegramToken, cfg.TelegramChatID, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create telegram client")
	}

	minioClient, err := storage.New(cfg, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to init minio client")
	}
	minioClient, err = minioClient.WaitForReady(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("minio not ready")
	}

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        cfg.KafkaBrokers,
		Topic:          cfg.KafkaTopic,
		GroupID:        cfg.KafkaConsumerGroup,
		MinBytes:       1,
		MaxBytes:       10e6,
		MaxWait:        cfg.KafkaReadTimeout,
		CommitInterval: 0,
	})
	defer reader.Close()

	healthSrv := health.NewServer(cfg.HealthAddr, log)
	healthSrv.AddLiveness("postgres", storeClient.Ping)
	healthSrv.AddLiveness("minio", minioClient.Ping)
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

	proc := processor.New(cfg, storeClient, reader, telegramClient, minioClient, log)
	log.Info().Str("topic", cfg.KafkaTopic).Str("group", cfg.KafkaConsumerGroup).Msg("game sender consumer started")
	if err := proc.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("processor stopped with error")
	}
}
