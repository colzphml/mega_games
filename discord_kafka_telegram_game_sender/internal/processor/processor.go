package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
	kafkago "github.com/segmentio/kafka-go"

	"github.com/colzphml/mega_games/discord_kafka_telegram_game_sender/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_telegram_game_sender/internal/storage"
	"github.com/colzphml/mega_games/discord_kafka_telegram_game_sender/internal/store"
	"github.com/colzphml/mega_games/discord_kafka_telegram_game_sender/internal/telegram"
)

type GameImageEvent struct {
	MessageID string `json:"message_id"`
	GameID    string `json:"game_id"`
	GameURL   string `json:"game_url"`
	ImageURL  string `json:"image_url"`
	Bucket    string `json:"bucket"`
	ObjectKey string `json:"object_key"`
}

type Processor struct {
	cfg     config.Config
	store   *store.Store
	reader  *kafkago.Reader
	client  *telegram.Client
	objects *storage.Client
	log     zerolog.Logger
}

func New(cfg config.Config, storeClient *store.Store, reader *kafkago.Reader, client *telegram.Client, objects *storage.Client, log zerolog.Logger) *Processor {
	return &Processor{cfg: cfg, store: storeClient, reader: reader, client: client, objects: objects, log: log}
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

	p.log.Info().Int("count", len(pending)).Msg("reprocessing pending game messages")
	for _, msg := range pending {
		if msg.Attempts >= p.cfg.MaxAttempts {
			if err := p.store.MoveToFailed(ctx, msg.ID, map[string]any{"reason": "max attempts", "source": source}); err != nil {
				p.log.Error().Err(err).Str("message_id", msg.ID).Msg("failed to move game message to failed table")
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
			p.log.Warn().Str("message_id", messageID).Msg("empty game payload")
			_ = p.reader.CommitMessages(ctx, msg)
			continue
		}

		if _, err := decodeEvent(payload); err != nil {
			p.log.Error().Err(err).Str("message_id", messageID).Msg("invalid game payload")
			_ = p.reader.CommitMessages(ctx, msg)
			continue
		}

		stored, err := p.store.EnsureMessage(ctx, messageID, payload)
		if err != nil {
			p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to persist game message")
			continue
		}

		if err := p.reader.CommitMessages(ctx, msg); err != nil {
			p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to commit kafka message")
		}

		if stored.Status == store.StatusProcessed() {
			p.log.Info().Str("message_id", messageID).Msg("game message already processed")
			continue
		}
		if stored.Status == store.StatusInProgress() {
			p.log.Info().Str("message_id", messageID).Msg("game message already in progress")
			continue
		}
		if stored.Attempts >= p.cfg.MaxAttempts {
			if err := p.store.MoveToFailed(ctx, messageID, kafkaDetails(msg, "max attempts on consume")); err != nil {
				p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to move game message to failed table")
			}
			continue
		}

		p.processMessage(ctx, stored, kafkaDetails(msg, "consume"))
	}
}

func (p *Processor) processMessage(ctx context.Context, msg store.GameMessage, details map[string]any) {
	if err := p.store.TouchAttempt(ctx, msg.ID, p.cfg.ProcessRetryInterval); err != nil {
		p.log.Info().Err(err).Str("message_id", msg.ID).Msg("game message not eligible for processing")
		return
	}
	if err := p.handleMessage(ctx, msg); err != nil {
		updated, updateErr := p.store.RecordAttempt(ctx, msg.ID, err.Error())
		if updateErr != nil {
			p.log.Error().Err(updateErr).Str("message_id", msg.ID).Msg("failed to record processing attempt")
			return
		}
		if updated.Attempts >= p.cfg.MaxAttempts {
			if moveErr := p.store.MoveToFailed(ctx, msg.ID, details); moveErr != nil {
				p.log.Error().Err(moveErr).Str("message_id", msg.ID).Msg("failed to move game message to failed table")
			}
			return
		}
		p.log.Error().Err(err).Str("message_id", msg.ID).Msg("failed to process game message")
		return
	}

	if err := p.store.MarkProcessed(ctx, msg.ID); err != nil {
		p.log.Error().Err(err).Str("message_id", msg.ID).Msg("failed to mark message processed")
	}
	p.log.Info().Str("message_id", msg.ID).Msg("game message processed")
}

func (p *Processor) handleMessage(ctx context.Context, msg store.GameMessage) error {
	event, err := decodeEvent(msg.Payload)
	if err != nil {
		return err
	}

	image, err := p.objects.Download(ctx, event.Bucket, event.ObjectKey)
	if err != nil {
		return err
	}

	caption := strings.TrimSpace(event.GameURL)
	if caption == "" {
		caption = strings.TrimSpace(event.ImageURL)
	}
	if caption == "" {
		caption = "game image"
	}

	if err := p.client.SendGameImage(caption, image); err != nil {
		return err
	}
	return nil
}

func decodeEvent(payload string) (GameImageEvent, error) {
	var event GameImageEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return GameImageEvent{}, fmt.Errorf("decode payload: %w", err)
	}
	if strings.TrimSpace(event.ObjectKey) == "" || strings.TrimSpace(event.Bucket) == "" {
		return GameImageEvent{}, fmt.Errorf("missing bucket or object_key")
	}
	if strings.TrimSpace(event.GameURL) == "" && strings.TrimSpace(event.ImageURL) == "" {
		return GameImageEvent{}, fmt.Errorf("missing game url")
	}
	return event, nil
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
