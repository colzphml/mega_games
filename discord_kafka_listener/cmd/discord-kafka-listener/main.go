package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/colzphml/mega_games/discord_kafka_listener/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_listener/internal/discord"
	"github.com/colzphml/mega_games/discord_kafka_listener/internal/kafka"
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
	log.Info().
		Str("version", buildVersion).
		Str("commit", buildCommit).
		Str("build_date", buildDate).
		Msg("build info")

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	messageIDs := make(chan string, cfg.MessageBufferSize)

	producer, err := kafka.NewProducer(cfg.KafkaBrokers, cfg.KafkaTopic, cfg.KafkaClientID, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create kafka producer")
	}
	defer producer.Close()

	discordClient, err := discord.NewClient(cfg.DiscordToken, cfg.DiscordChannelID, messageIDs, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create discord client")
	}

	if err := discordClient.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to start discord client")
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case messageID := <-messageIDs:
				writeCtx, cancel := context.WithTimeout(ctx, cfg.KafkaWriteTimeout)
				err := producer.WriteMessage(writeCtx, messageID)
				cancel()
				if err != nil {
					log.Error().Err(err).Str("message_id", messageID).Msg("failed to write to kafka")
					continue
				}
				log.Info().Str("message_id", messageID).Msg("sent message id to kafka")
			}
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutdown requested")

	wg.Wait()
}
