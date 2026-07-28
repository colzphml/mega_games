package main

import (
	"context"
	"errors"
	"net"
	"net/http"
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

	if err := waitForPostgres(ctx, pgStore, cfg, log); err != nil {
		log.Fatal().Err(err).Msg("postgres not ready")
	}
	if err := pgStore.EnsureSchema(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to ensure postgres schema")
	}

	objectStore, err := waitForMinio(ctx, cfg, log)
	if err != nil {
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

	healthServer := startHealthServer(ctx, cfg.HealthAddr, pgStore, objectStore, cfg.KafkaBrokers, log)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = healthServer.Shutdown(shutdownCtx)
	}()

	proc := processor.New(cfg, pgStore, reader, producer, fetcher, objectStore, log)
	log.Info().Str("topic", cfg.KafkaGameTopic).Str("group", cfg.KafkaConsumerGroup).Msg("game image consumer started")
	if err := proc.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("processor stopped with error")
	}
}

func startHealthServer(ctx context.Context, addr string, pgStore *pgstore.Store, objectStore *storage.Client, brokers []string, log zerolog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pgStore.Ping(pingCtx); err != nil {
			http.Error(w, "postgres not healthy", http.StatusServiceUnavailable)
			return
		}
		if err := objectStore.Ping(pingCtx); err != nil {
			http.Error(w, "minio not healthy", http.StatusServiceUnavailable)
			return
		}
		if err := kafkaPing(r.Context(), brokers); err != nil {
			http.Error(w, "kafka not healthy", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Info().Str("addr", addr).Msg("health server listening")

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("health server stopped")
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("health server shutdown error")
		}
	}()

	return srv
}

func kafkaPing(ctx context.Context, brokers []string) error {
	if len(brokers) == 0 {
		return errors.New("no kafka brokers configured")
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		return err
	}
	return conn.Close()
}

func waitForPostgres(ctx context.Context, pgStore *pgstore.Store, cfg config.Config, log zerolog.Logger) error {
	for attempt := 1; attempt <= cfg.PostgresMaxAttempts; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, cfg.PostgresTimeout)
		err := pgStore.Ping(pingCtx)
		cancel()
		if err == nil {
			return nil
		}
		if attempt == cfg.PostgresMaxAttempts {
			return err
		}
		log.Warn().Err(err).Int("attempt", attempt).Int("max_attempts", cfg.PostgresMaxAttempts).Msg("postgres not ready, retrying")
		if !sleepWithContext(ctx, cfg.PostgresRetryDelay) {
			return ctx.Err()
		}
	}
	return nil
}

func waitForMinio(ctx context.Context, cfg config.Config, log zerolog.Logger) (*storage.Client, error) {
	for attempt := 1; attempt <= cfg.MinioMaxAttempts; attempt++ {
		client, err := storage.New(ctx, cfg, log)
		if err == nil {
			return client, nil
		}
		if attempt == cfg.MinioMaxAttempts {
			return nil, err
		}
		log.Warn().Err(err).Int("attempt", attempt).Int("max_attempts", cfg.MinioMaxAttempts).Msg("minio not ready, retrying")
		if !sleepWithContext(ctx, cfg.MinioRetryDelay) {
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("minio not ready")
}

func sleepWithContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
