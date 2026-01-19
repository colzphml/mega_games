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

	"github.com/colzphml/mega_games/discord_kafka_telegram_week_sender/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_telegram_week_sender/internal/processor"
	"github.com/colzphml/mega_games/discord_kafka_telegram_week_sender/internal/store"
	"github.com/colzphml/mega_games/discord_kafka_telegram_week_sender/internal/telegram"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	log := zerolog.New(os.Stdout).With().Str("service", "discord-kafka-telegram-week-sender").Timestamp().Logger()

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

	if err := waitForPostgres(ctx, storeClient, cfg, log); err != nil {
		log.Fatal().Err(err).Msg("postgres not ready")
	}
	if err := storeClient.EnsureSchema(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to ensure postgres schema")
	}

	telegramClient, err := telegram.New(cfg.TelegramToken, cfg.TelegramChatID, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create telegram client")
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

	healthServer := startHealthServer(ctx, cfg.HealthAddr, storeClient, cfg.KafkaBrokers, log)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = healthServer.Shutdown(shutdownCtx)
	}()

	proc := processor.New(cfg, storeClient, reader, telegramClient, log)
	log.Info().Str("topic", cfg.KafkaTopic).Str("group", cfg.KafkaConsumerGroup).Msg("week sender consumer started")
	if err := proc.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("processor stopped with error")
	}
}

func startHealthServer(ctx context.Context, addr string, storeClient *store.Store, brokers []string, log zerolog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := storeClient.Ping(pingCtx); err != nil {
			http.Error(w, "postgres not healthy", http.StatusServiceUnavailable)
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

func waitForPostgres(ctx context.Context, storeClient *store.Store, cfg config.Config, log zerolog.Logger) error {
	for attempt := 1; attempt <= cfg.PostgresMaxAttempts; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, cfg.PostgresTimeout)
		err := storeClient.Ping(pingCtx)
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
