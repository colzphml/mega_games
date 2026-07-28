package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/colzphml/mega_games/discord_kafka_listener/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_listener/internal/discord"
	"github.com/colzphml/mega_games/discord_kafka_listener/internal/kafka"
	"github.com/colzphml/mega_games/discord_kafka_listener/internal/state"
	"github.com/colzphml/mega_games/internal/common/health"
	"github.com/rs/zerolog"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	log := zerolog.New(os.Stdout).With().Str("service", "discord-kafka-listener").Timestamp().Logger()

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

	messageIDs := make(chan string, cfg.MessageBufferSize)

	producer, err := kafka.NewProducer(cfg.KafkaBrokers, cfg.KafkaInputTopic, cfg.KafkaClientID, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create kafka producer")
	}
	defer producer.Close()

	store := state.NewLastMessageStore(cfg.LastMessageIDPath, log)

	bufferedWriter := kafka.NewBufferedWriter(producer, kafka.BufferedWriterConfig{
		WriteTimeout: cfg.KafkaWriteTimeout,
		BaseDelay:    cfg.KafkaRetryBaseDelay,
		MaxDelay:     cfg.KafkaRetryMaxDelay,
		MaxAttempts:  cfg.KafkaRetryMaxAttempts,
		MaxPending:   cfg.KafkaRetryMaxPending,
		BufferSize:   cfg.MessageBufferSize,
	}, log)

	discordClient, err := discord.NewClient(cfg.DiscordToken, cfg.DiscordChannelID, messageIDs, cfg.MessageEnqueueTimeout, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create discord client")
	}

	if err := discordClient.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to start discord client")
	}

	// Liveness is intentionally empty: this service is alive as long as
	// the HTTP server answers. Readiness to actually do work depends on
	// the Discord session, which belongs on /ready only.
	healthSrv := health.NewServer(cfg.HealthAddr, log)
	healthSrv.AddReadiness("discord", func(ctx context.Context) error {
		if !discordClient.Ready() {
			return errors.New("discord session not ready")
		}
		return nil
	})
	healthSrv.Start(ctx)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = healthSrv.Shutdown(shutdownCtx)
	}()

	var backfillMu sync.Mutex
	backfillInProgress := false
	lastBackfill := time.Time{}

	triggerBackfill := func(reason discord.EventType) {
		if cfg.BackfillMaxMessages == 0 {
			return
		}
		lastID := store.Get()
		if lastID == "" {
			return
		}
		backfillMu.Lock()
		if backfillInProgress {
			backfillMu.Unlock()
			return
		}
		if !lastBackfill.IsZero() && time.Since(lastBackfill) < cfg.BackfillMinInterval {
			backfillMu.Unlock()
			return
		}
		backfillInProgress = true
		lastBackfill = time.Now()
		backfillMu.Unlock()

		go func() {
			defer func() {
				backfillMu.Lock()
				backfillInProgress = false
				backfillMu.Unlock()
			}()
			backfillCtx, cancel := context.WithTimeout(ctx, cfg.BackfillTimeout)
			defer cancel()
			count, err := discordClient.BackfillSince(backfillCtx, lastID, cfg.BackfillPageSize, cfg.BackfillMaxMessages)
			if err != nil {
				log.Warn().Err(err).Str("reason", string(reason)).Msg("discord backfill failed")
				return
			}
			if count > 0 {
				log.Info().Str("reason", string(reason)).Int("count", count).Msg("discord backfill completed")
			}
		}()
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-discordClient.Events():
				switch ev.Type {
				case discord.EventReady, discord.EventResumed:
					triggerBackfill(ev.Type)
				}
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		bufferedWriter.Run(ctx, func(messageID string) {
			store.UpdateIfNewer(messageID)
			log.Info().Str("message_id", messageID).Msg("sent message id to kafka")
		})
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case messageID := <-messageIDs:
				if ok := bufferedWriter.Enqueue(ctx, messageID, cfg.MessageEnqueueTimeout); !ok {
					log.Warn().Str("message_id", messageID).Msg("retry queue full, dropping")
				}
			}
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutdown requested")

	wg.Wait()
}
