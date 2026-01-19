package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	kafkago "github.com/segmentio/kafka-go"

	"github.com/colzphml/mega_games/discord_kafka_week_formatter/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_week_formatter/internal/formatter"
	"github.com/colzphml/mega_games/discord_kafka_week_formatter/internal/kafka"
	"github.com/colzphml/mega_games/discord_kafka_week_formatter/internal/store"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	log := zerolog.New(os.Stdout).With().Str("service", "discord-kafka-week-formatter").Timestamp().Logger()

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

	producer, err := kafka.NewProducer(cfg.KafkaBrokers, cfg.KafkaTelegramTopic, cfg.KafkaClientID, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create kafka producer")
	}
	defer producer.Close()

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        cfg.KafkaBrokers,
		Topic:          cfg.KafkaWeekTopic,
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

	proc := NewProcessor(cfg, storeClient, reader, producer, log)
	log.Info().Str("topic", cfg.KafkaWeekTopic).Str("group", cfg.KafkaConsumerGroup).Msg("week formatter consumer started")
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

// Processor implementation

type Processor struct {
	cfg    config.Config
	store  *store.Store
	reader *kafkago.Reader
	writer *kafka.Producer
	log    zerolog.Logger
}

func NewProcessor(cfg config.Config, storeClient *store.Store, reader *kafkago.Reader, writer *kafka.Producer, log zerolog.Logger) *Processor {
	return &Processor{
		cfg:    cfg,
		store:  storeClient,
		reader: reader,
		writer: writer,
		log:    log,
	}
}

func (p *Processor) Run(ctx context.Context) error {
	if err := p.reprocessPending(ctx, 0, "startup"); err != nil {
		return err
	}
	if p.cfg.ProcessRetryInterval > 0 {
		go p.retryLoop(ctx)
	}
	return p.consumeLoop(ctx)
}

func (p *Processor) retryLoop(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.ProcessRetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.reprocessPending(ctx, p.cfg.ProcessRetryInterval, "interval"); err != nil {
				p.log.Error().Err(err).Msg("failed to reprocess pending messages")
			}
		}
	}
}

func (p *Processor) reprocessPending(ctx context.Context, retryAfter time.Duration, source string) error {
	pending, err := p.store.ListPending(ctx, 1000, retryAfter)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	p.log.Info().Int("count", len(pending)).Msg("reprocessing pending week messages")
	for _, msg := range pending {
		if msg.Attempts >= p.cfg.MaxAttempts {
			if err := p.store.MoveToFailed(ctx, msg.ID, map[string]any{"reason": "max attempts", "source": source}); err != nil {
				p.log.Error().Err(err).Str("message_id", msg.ID).Msg("failed to move week message to failed table")
			}
			continue
		}
		p.processMessage(ctx, msg.ID, msg.Payload, map[string]any{"source": source})
	}
	return nil
}

func (p *Processor) consumeLoop(ctx context.Context) error {
	for {
		msg, err := p.reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			p.log.Error().Err(err).Msg("failed to fetch kafka message")
			continue
		}

		messageID := string(msg.Key)
		if messageID == "" {
			messageID = fmt.Sprintf("%s:%d:%d", msg.Topic, msg.Partition, msg.Offset)
		}
		p.log.Info().Str("message_id", messageID).Str("topic", msg.Topic).Msg("week message received")

		payload, err := decodePayload(msg.Value)
		if err != nil {
			p.log.Error().Err(err).Str("message_id", messageID).Msg("invalid week payload")
			_ = p.reader.CommitMessages(ctx, msg)
			continue
		}

		stored, err := p.store.EnsureWeekMessage(ctx, messageID, payload)
		if err != nil {
			p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to persist week message")
			continue
		}

		if err := p.reader.CommitMessages(ctx, msg); err != nil {
			p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to commit kafka message")
		}

		if stored.Status == store.StatusProcessed() {
			p.log.Info().Str("message_id", messageID).Msg("week message already processed")
			continue
		}
		if stored.Attempts >= p.cfg.MaxAttempts {
			if err := p.store.MoveToFailed(ctx, messageID, kafkaDetails(msg, "max attempts on consume")); err != nil {
				p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to move week message to failed table")
			}
			continue
		}

		p.processMessage(ctx, messageID, payload, kafkaDetails(msg, "consume"))
	}
}

func (p *Processor) processMessage(ctx context.Context, messageID string, payload store.WeekPayload, details map[string]any) {
	if err := p.handleMessage(ctx, messageID, payload); err != nil {
		updated, updateErr := p.store.RecordAttempt(ctx, messageID, err.Error())
		if updateErr != nil {
			p.log.Error().Err(updateErr).Str("message_id", messageID).Msg("failed to record processing attempt")
			return
		}
		if updated.Attempts >= p.cfg.MaxAttempts {
			if moveErr := p.store.MoveToFailed(ctx, messageID, details); moveErr != nil {
				p.log.Error().Err(moveErr).Str("message_id", messageID).Msg("failed to move week message to failed table")
			}
			return
		}
		p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to process week message")
	}
}

func (p *Processor) handleMessage(ctx context.Context, messageID string, payload store.WeekPayload) error {
	teams, err := p.store.LoadTeams(ctx)
	if err != nil {
		return err
	}
	games, err := p.store.LoadGamesForWeek(ctx, payload.Season, payload.Week)
	if err != nil {
		return err
	}

	message, err := formatter.BuildWeekMessage(payload, teams, games)
	if err != nil {
		return err
	}

	writeCtx, cancel := context.WithTimeout(ctx, p.cfg.KafkaWriteTimeout)
	err = p.writer.Write(writeCtx, messageID, message)
	cancel()
	if err != nil {
		return err
	}

	if err := p.store.MarkProcessed(ctx, messageID); err != nil {
		return err
	}
	p.log.Info().Str("message_id", messageID).Msg("week message processed")
	return nil
}

func decodePayload(value []byte) (store.WeekPayload, error) {
	var payload store.WeekPayload
	if err := json.Unmarshal(value, &payload); err != nil {
		return store.WeekPayload{}, err
	}
	if payload.Season == "" {
		return store.WeekPayload{}, fmt.Errorf("missing season")
	}
	if payload.Week == 0 && payload.Title == "" {
		return store.WeekPayload{}, fmt.Errorf("missing week or title")
	}
	return payload, nil
}

func kafkaDetails(msg kafkago.Message, reason string) map[string]any {
	return map[string]any{
		"topic":     msg.Topic,
		"partition": msg.Partition,
		"offset":    msg.Offset,
		"timestamp": msg.Time,
		"key":       string(msg.Key),
		"reason":    reason,
	}
}
