package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"
	kafkago "github.com/segmentio/kafka-go"

	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/kafka"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/middle"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/storage"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/store"
)

type Processor struct {
	cfg     config.Config
	store   *store.Store
	reader  *kafkago.Reader
	writer  *kafka.Producer
	fetcher middle.Fetcher
	objects *storage.Client
	log     zerolog.Logger
}

type GameImageEvent struct {
	MessageID       string    `json:"message_id"`
	SourceMessageID string    `json:"source_message_id,omitempty"`
	GameID          string    `json:"game_id"`
	GameURL         string    `json:"game_url"`
	ImageURL        string    `json:"image_url"`
	Bucket          string    `json:"bucket"`
	ObjectKey       string    `json:"object_key"`
	ContentType     string    `json:"content_type"`
	Size            int64     `json:"size"`
	StoredAt        time.Time `json:"stored_at"`
	Fetcher         string    `json:"fetcher"`
}

func New(cfg config.Config, storeClient *store.Store, reader *kafkago.Reader, writer *kafka.Producer, fetcher middle.Fetcher, objects *storage.Client, log zerolog.Logger) *Processor {
	return &Processor{
		cfg:     cfg,
		store:   storeClient,
		reader:  reader,
		writer:  writer,
		fetcher: fetcher,
		objects: objects,
		log:     log,
	}
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

	p.log.Info().Int("count", len(pending)).Msg("reprocessing pending game images")
	for _, msg := range pending {
		if msg.Attempts >= p.cfg.MaxAttempts {
			if err := p.store.MoveToFailed(ctx, msg.ID, map[string]any{"reason": "max attempts", "source": source}); err != nil {
				p.log.Error().Err(err).Str("message_id", msg.ID).Msg("failed to move game message to failed collection")
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

		rawKey := string(msg.Key)
		gameValue := strings.TrimSpace(string(msg.Value))
		if gameValue == "" {
			p.log.Warn().Str("topic", msg.Topic).Msg("empty game payload")
			_ = p.reader.CommitMessages(ctx, msg)
			continue
		}

		payload, err := parseGamePayload(gameValue, p.cfg.BaseURL, p.cfg.League)
		if err != nil {
			p.log.Error().Err(err).Str("payload", gameValue).Msg("invalid game payload")
			_ = p.reader.CommitMessages(ctx, msg)
			continue
		}

		messageID := composeMessageID(rawKey, payload.GameID, msg)
		p.log.Info().Str("message_id", messageID).Str("game_id", payload.GameID).Msg("game message received")

		stored, err := p.store.EnsureGameMessage(ctx, messageID, payload)
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
		if stored.Attempts >= p.cfg.MaxAttempts {
			if err := p.store.MoveToFailed(ctx, messageID, kafkaDetails(msg, "max attempts on consume")); err != nil {
				p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to move game message to failed collection")
			}
			continue
		}

		p.processMessage(ctx, stored, kafkaDetails(msg, "consume"))
	}
}

func (p *Processor) processMessage(ctx context.Context, msg store.GameMessage, details map[string]any) {
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
				p.log.Error().Err(moveErr).Str("message_id", msg.ID).Msg("failed to move game message to failed collection")
			}
			return
		}
		p.log.Error().Err(err).Str("message_id", msg.ID).Msg("failed to process game message")
	}
}

func (p *Processor) handleMessage(ctx context.Context, msg store.GameMessage) error {
	payload := msg.Payload
	if payload.GameID == "" || payload.GameURL == "" {
		return fmt.Errorf("missing game payload")
	}

	meta := msg.Image
	if meta == nil {
		fetchCtx, cancel := context.WithTimeout(ctx, p.cfg.FetchTimeout)
		result, err := p.fetcher.Fetch(fetchCtx, payload.GameID)
		cancel()
		if err != nil {
			return err
		}
		contentType := result.ContentType
		if contentType == "" {
			contentType = http.DetectContentType(result.Image)
		}
		ext := extensionForContentType(contentType)
		objectKey := p.objects.ObjectKey(payload.GameID, msg.ID, ext)

		uploadCtx, uploadCancel := context.WithTimeout(ctx, p.cfg.MinioUploadTimeout)
		upload, err := p.objects.Upload(uploadCtx, objectKey, contentType, result.Image)
		uploadCancel()
		if err != nil {
			return err
		}

		meta = &store.ImageMeta{
			ImageURL:    upload.URL,
			Bucket:      upload.Bucket,
			ObjectKey:   upload.ObjectKey,
			ContentType: upload.ContentType,
			Size:        upload.Size,
			Fetcher:     p.cfg.FetcherType,
			StoredAt:    time.Now(),
		}
		if err := p.store.UpdateMetadata(ctx, msg.ID, *meta); err != nil {
			return err
		}
	}

	event := GameImageEvent{
		MessageID:       msg.ID,
		SourceMessageID: extractSourceMessageID(msg.ID),
		GameID:          payload.GameID,
		GameURL:         payload.GameURL,
		ImageURL:        meta.ImageURL,
		Bucket:          meta.Bucket,
		ObjectKey:       meta.ObjectKey,
		ContentType:     meta.ContentType,
		Size:            meta.Size,
		StoredAt:        meta.StoredAt,
		Fetcher:         p.cfg.FetcherType,
	}
	payloadBytes, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal game image event: %w", err)
	}

	writeCtx, cancel := context.WithTimeout(ctx, p.cfg.KafkaWriteTimeout)
	err = p.writer.Write(writeCtx, msg.ID, payloadBytes)
	cancel()
	if err != nil {
		return err
	}

	if err := p.store.MarkProcessed(ctx, msg.ID); err != nil {
		return err
	}

	p.log.Info().Str("message_id", msg.ID).Str("game_id", payload.GameID).Msg("game image processed")
	return nil
}

func parseGamePayload(value, baseURL, league string) (store.GamePayload, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return store.GamePayload{}, fmt.Errorf("empty payload")
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return store.GamePayload{}, fmt.Errorf("invalid url payload: %w", err)
		}
		gameID, parsedLeague := parseGameFromPath(u.Path)
		if gameID == "" {
			return store.GamePayload{}, fmt.Errorf("cannot parse game id from url")
		}
		if parsedLeague != "" {
			league = parsedLeague
		}
		return store.GamePayload{
			GameID:  gameID,
			GameURL: trimmed,
		}, nil
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" || league == "" {
		return store.GamePayload{}, fmt.Errorf("missing base url or league")
	}
	gameID := trimmed
	gameURL := fmt.Sprintf("%s/leagues/%s/games/%s", baseURL, league, gameID)
	return store.GamePayload{GameID: gameID, GameURL: gameURL}, nil
}

func parseGameFromPath(p string) (gameID, league string) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) >= 4 && parts[0] == "leagues" && parts[2] == "games" {
		return parts[3], parts[1]
	}
	return "", ""
}

func composeMessageID(rawKey, gameID string, msg kafkago.Message) string {
	base := strings.TrimSpace(rawKey)
	if base == "" {
		base = fmt.Sprintf("%s:%d:%d", msg.Topic, msg.Partition, msg.Offset)
	}
	if gameID == "" {
		return base
	}
	return fmt.Sprintf("%s:%s", base, gameID)
}

func extractSourceMessageID(messageID string) string {
	if messageID == "" {
		return ""
	}
	parts := strings.SplitN(messageID, ":", 2)
	return parts[0]
}

func extensionForContentType(contentType string) string {
	typ := strings.ToLower(contentType)
	switch {
	case strings.Contains(typ, "png"):
		return ".png"
	case strings.Contains(typ, "jpeg"), strings.Contains(typ, "jpg"):
		return ".jpg"
	default:
		return ".bin"
	}
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
