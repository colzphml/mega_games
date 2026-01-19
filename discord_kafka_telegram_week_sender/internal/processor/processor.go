package processor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
	kafkago "github.com/segmentio/kafka-go"

	"github.com/colzphml/mega_games/discord_kafka_telegram_week_sender/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_telegram_week_sender/internal/store"
	"github.com/colzphml/mega_games/discord_kafka_telegram_week_sender/internal/telegram"
)

type Processor struct {
	cfg    config.Config
	store  *store.Store
	reader *kafkago.Reader
	client *telegram.Client
	log    zerolog.Logger
}

func New(cfg config.Config, storeClient *store.Store, reader *kafkago.Reader, client *telegram.Client, log zerolog.Logger) *Processor {
	return &Processor{cfg: cfg, store: storeClient, reader: reader, client: client, log: log}
}

func (p *Processor) Run(ctx context.Context) error {
	if err := p.reprocessPending(ctx, p.cfg.ProcessRetryInterval, "startup"); err != nil {
		p.log.Error().Err(err).Msg("failed to reprocess pending messages")
	}

	retryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go p.retryLoop(retryCtx)

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
		p.processMessage(ctx, msg, map[string]any{"source": source})
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

		payload := strings.TrimSpace(string(msg.Value))
		if payload == "" {
			p.log.Warn().Str("message_id", messageID).Msg("empty week payload")
			_ = p.reader.CommitMessages(ctx, msg)
			continue
		}

		stored, err := p.store.EnsureMessage(ctx, messageID, payload)
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

		p.processMessage(ctx, stored, kafkaDetails(msg, "consume"))
	}
}

func (p *Processor) processMessage(ctx context.Context, msg store.WeekMessage, details map[string]any) {
	if err := p.store.TouchAttempt(ctx, msg.ID); err != nil {
		p.log.Warn().Err(err).Str("message_id", msg.ID).Msg("failed to mark attempt start")
	}
	if err := p.handleMessage(ctx, msg); err != nil {
		updated, updateErr := p.store.RecordAttempt(ctx, msg.ID, err.Error())
		if updateErr != nil {
			p.log.Error().Err(updateErr).Str("message_id", msg.ID).Msg("failed to record processing attempt")
			return
		}
		if updated.Attempts >= p.cfg.MaxAttempts {
			if moveErr := p.store.MoveToFailed(ctx, msg.ID, details); moveErr != nil {
				p.log.Error().Err(moveErr).Str("message_id", msg.ID).Msg("failed to move week message to failed table")
			}
			return
		}
		p.log.Error().Err(err).Str("message_id", msg.ID).Msg("failed to process week message")
		return
	}

	if err := p.store.MarkProcessed(ctx, msg.ID); err != nil {
		p.log.Error().Err(err).Str("message_id", msg.ID).Msg("failed to mark message processed")
	}
	p.log.Info().Str("message_id", msg.ID).Msg("week message processed")
}

func (p *Processor) handleMessage(ctx context.Context, msg store.WeekMessage) error {
	if err := p.client.SendWeekMessage(msg.Payload); err != nil {
		return err
	}
	return nil
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
