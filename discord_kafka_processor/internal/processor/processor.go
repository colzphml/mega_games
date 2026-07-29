package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
	kafkago "github.com/segmentio/kafka-go"

	"github.com/colzphml/mega_games/discord_kafka_processor/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_processor/internal/kafka"
	"github.com/colzphml/mega_games/discord_kafka_processor/internal/parser"
	"github.com/colzphml/mega_games/discord_kafka_processor/internal/store"
	"github.com/colzphml/mega_games/internal/common/ports"
	"github.com/colzphml/mega_games/internal/common/retry"
)

var errMessageNotReady = errors.New("discord message not ready")

type Processor struct {
	cfg      config.Config
	store    *store.Store
	discord  ports.DiscordMessages
	producer *kafka.Producer
	reader   *kafkago.Reader
	log      zerolog.Logger
}

func New(cfg config.Config, store *store.Store, discordClient ports.DiscordMessages, producer *kafka.Producer, reader *kafkago.Reader, log zerolog.Logger) *Processor {
	return &Processor{
		cfg:      cfg,
		store:    store,
		discord:  discordClient,
		producer: producer,
		reader:   reader,
		log:      log,
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

func (p *Processor) reprocessPending(ctx context.Context, retryAfter time.Duration, source string) error {
	pending, err := p.store.ListPending(ctx, 1000, retryAfter)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	p.log.Info().Int("count", len(pending)).Msg("reprocessing pending messages")
	for _, msg := range pending {
		if msg.Attempts >= p.cfg.MaxAttempts {
			if err := p.store.MoveToFailed(ctx, msg.ID, map[string]any{"reason": "max attempts", "source": source}); err != nil {
				p.log.Error().Err(err).Str("message_id", msg.ID).Msg("failed to move message to failed table")
			}
			continue
		}

		p.processMessage(ctx, msg.ID, map[string]any{"source": source})
	}
	return nil
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

		messageID := strings.TrimSpace(string(msg.Value))
		if messageID == "" {
			p.log.Warn().Msg("empty kafka message received, skipping")
			_ = p.reader.CommitMessages(ctx, msg)
			continue
		}

		stored, err := p.store.EnsureMessage(ctx, messageID)
		if err != nil {
			p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to persist message id")
			continue
		}

		if err := p.reader.CommitMessages(ctx, msg); err != nil {
			p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to commit kafka message")
		}

		if stored.CurrentState == store.StatusProcessed() {
			p.log.Info().Str("message_id", messageID).Msg("message already processed")
			continue
		}

		if stored.Attempts >= p.cfg.MaxAttempts {
			if err := p.store.MoveToFailed(ctx, messageID, kafkaDetails(msg, "max attempts on consume")); err != nil {
				p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to move message to failed table")
			}
			continue
		}

		p.processMessage(ctx, messageID, kafkaDetails(msg, "consume"))
	}
}

// shouldChargeAttempt reports whether a failure counts against
// PROCESS_MAX_ATTEMPTS. Transient failures do not: otherwise a
// prolonged Discord outage would exhaust the budget and move perfectly
// good messages to the failed table for good.
func shouldChargeAttempt(err error) bool {
	return retry.Classify(err) != retry.Transient
}

func (p *Processor) processMessage(ctx context.Context, messageID string, details map[string]any) {
	err := p.handleMessage(ctx, messageID)
	if err == nil {
		return
	}

	if !shouldChargeAttempt(err) {
		p.log.Warn().Err(err).Str("message_id", messageID).
			Str("error_kind", retry.Classify(err).String()).
			Msg("transient failure, message stays queued without consuming an attempt")
		return
	}

	updated, updateErr := p.store.RecordAttempt(ctx, messageID, err.Error())
	if updateErr != nil {
		p.log.Error().Err(updateErr).Str("message_id", messageID).Msg("failed to record processing attempt")
		return
	}
	if updated.Attempts >= p.cfg.MaxAttempts {
		if moveErr := p.store.MoveToFailed(ctx, messageID, details); moveErr != nil {
			p.log.Error().Err(moveErr).Str("message_id", messageID).Msg("failed to move message to failed table")
		}
		return
	}
	p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to process message")
}

func (p *Processor) handleMessage(ctx context.Context, messageID string) error {
	result, err := p.fetchAndParse(ctx, messageID)
	if err != nil {
		return err
	}

	if result.Skip {
		if err := p.store.MarkProcessed(ctx, messageID); err != nil {
			return err
		}
		p.log.Info().Str("message_id", messageID).Msg("message skipped")
		return nil
	}

	if result.Week != nil {
		payload, err := json.Marshal(result.Week)
		if err != nil {
			return fmt.Errorf("marshal week update: %w", err)
		}
		writeCtx, cancel := context.WithTimeout(ctx, p.cfg.KafkaWriteTimeout)
		err = p.producer.WriteWeek(writeCtx, messageID, payload)
		cancel()
		if err != nil {
			return err
		}
		event := p.log.Info().Str("message_id", messageID).Str("season", result.Week.Season)
		if result.Week.Week > 0 {
			event = event.Int("week", result.Week.Week)
		} else if result.Week.Title != "" {
			event = event.Str("title", result.Week.Title)
		}
		event.Msg("week update sent")
	}

	for _, game := range result.Games {
		writeCtx, cancel := context.WithTimeout(ctx, p.cfg.KafkaWriteTimeout)
		err := p.producer.WriteGame(writeCtx, messageID, game)
		cancel()
		if err != nil {
			return err
		}
		p.log.Info().Str("message_id", messageID).Str("game", game).Msg("game update sent")
	}

	if err := p.store.MarkProcessed(ctx, messageID); err != nil {
		return err
	}
	return nil
}

type parseResult struct {
	Week  *parser.WeekUpdate
	Games []string
	Skip  bool
}

func (p *Processor) fetchAndParse(ctx context.Context, messageID string) (parseResult, error) {
	var lastErr error
	for attempt := 1; attempt <= p.cfg.FetchAttempts; attempt++ {
		fetchCtx, cancel := context.WithTimeout(ctx, p.cfg.FetchTimeout)
		msg, err := p.discord.FetchMessage(fetchCtx, messageID)
		cancel()
		if err != nil {
			lastErr = err
			if !retry.Sleep(ctx, p.cfg.FetchDelay) {
				return parseResult{}, ctx.Err()
			}
			continue
		}

		parsed := parser.ParseMessage(msg)
		if !parsed.Ready {
			if isMessagePastWarmup(msg, p.cfg.EmbedWarmup) {
				return parseResult{Skip: true}, nil
			}
			lastErr = errMessageNotReady
			if !retry.Sleep(ctx, p.cfg.FetchDelay) {
				return parseResult{}, ctx.Err()
			}
			continue
		}

		if parsed.Week == nil && len(parsed.Games) == 0 {
			return parseResult{Skip: true}, nil
		}

		return parseResult{Week: parsed.Week, Games: parsed.Games}, nil
	}

	if lastErr == nil {
		lastErr = errMessageNotReady
	}
	return parseResult{}, lastErr
}

func isMessagePastWarmup(msg *discordgo.Message, warmup time.Duration) bool {
	if msg == nil || msg.Timestamp.IsZero() {
		return false
	}
	return time.Since(msg.Timestamp) >= warmup
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
