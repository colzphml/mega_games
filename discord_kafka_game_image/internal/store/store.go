package store

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/config"
)

const (
	statusNew       = "new"
	statusProcessed = "processed"
)

type GamePayload struct {
	GameID  string `bson:"game_id" json:"game_id"`
	GameURL string `bson:"game_url" json:"game_url"`
}

type ImageMeta struct {
	ImageURL    string    `bson:"image_url" json:"image_url"`
	Bucket      string    `bson:"image_bucket" json:"image_bucket"`
	ObjectKey   string    `bson:"image_object" json:"image_object"`
	ContentType string    `bson:"content_type" json:"content_type"`
	Size        int64     `bson:"size" json:"size"`
	Fetcher     string    `bson:"fetcher" json:"fetcher"`
	StoredAt    time.Time `bson:"stored_at" json:"stored_at"`
}

type GameMessage struct {
	ID            string      `bson:"_id"`
	Status        string      `bson:"status"`
	Attempts      int         `bson:"attempts"`
	Payload       GamePayload `bson:"payload"`
	CreatedAt     time.Time   `bson:"created_at"`
	UpdatedAt     time.Time   `bson:"updated_at"`
	LastAttemptAt *time.Time  `bson:"last_attempt_at,omitempty"`
	LastError     string      `bson:"last_error,omitempty"`
	Image         *ImageMeta  `bson:"image,omitempty"`
}

type Store struct {
	client     *mongo.Client
	collection *mongo.Collection
	failed     *mongo.Collection
	log        zerolog.Logger
}

func New(ctx context.Context, cfg config.Config, log zerolog.Logger) (*Store, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI).SetConnectTimeout(cfg.MongoConnectTimeout))
	if err != nil {
		return nil, fmt.Errorf("connect to mongo: %w", err)
	}

	store := &Store{
		client:     client,
		collection: client.Database(cfg.MongoDB).Collection(cfg.MongoCollection),
		failed:     client.Database(cfg.MongoDB).Collection(cfg.MongoFailedCollection),
		log:        log,
	}

	if err := store.ensureIndexes(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.client.Ping(ctx, readpref.Primary()); err != nil {
		return fmt.Errorf("mongo ping: %w", err)
	}
	return nil
}

func (s *Store) ensureIndexes(ctx context.Context) error {
	_, err := s.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "created_at", Value: 1}}},
		{Keys: bson.D{{Key: "payload.game_id", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("ensure mongo indexes: %w", err)
	}
	return nil
}

func (s *Store) EnsureGameMessage(ctx context.Context, messageID string, payload GamePayload) (GameMessage, error) {
	now := time.Now()
	_, err := s.collection.UpdateByID(
		ctx,
		messageID,
		bson.M{
			"$setOnInsert": bson.M{
				"_id":        messageID,
				"status":     statusNew,
				"attempts":   0,
				"payload":    payload,
				"created_at": now,
				"updated_at": now,
			},
		},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return GameMessage{}, fmt.Errorf("insert game message: %w", err)
	}
	return s.getMessage(ctx, messageID)
}

func (s *Store) getMessage(ctx context.Context, messageID string) (GameMessage, error) {
	var msg GameMessage
	if err := s.collection.FindOne(ctx, bson.M{"_id": messageID}).Decode(&msg); err != nil {
		return GameMessage{}, fmt.Errorf("get game message: %w", err)
	}
	return msg, nil
}

func (s *Store) ListPending(ctx context.Context, limit int, retryAfter time.Duration) ([]GameMessage, error) {
	if limit <= 0 {
		limit = 1000
	}
	filter := bson.M{"status": statusNew}
	if retryAfter > 0 {
		cutoff := time.Now().Add(-retryAfter)
		filter["$or"] = []bson.M{
			{"last_attempt_at": bson.M{"$exists": false}},
			{"last_attempt_at": nil},
			{"last_attempt_at": bson.M{"$lte": cutoff}},
		}
	}

	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}).SetLimit(int64(limit))
	cursor, err := s.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer cursor.Close(ctx)

	var messages []GameMessage
	for cursor.Next(ctx) {
		var msg GameMessage
		if err := cursor.Decode(&msg); err != nil {
			return nil, fmt.Errorf("decode pending: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, cursor.Err()
}

func (s *Store) RecordAttempt(ctx context.Context, messageID string, errMsg string) (GameMessage, error) {
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	update := bson.M{
		"$inc": bson.M{"attempts": 1},
		"$set": bson.M{
			"last_error":      errMsg,
			"last_attempt_at": time.Now(),
			"updated_at":      time.Now(),
		},
	}
	var msg GameMessage
	if err := s.collection.FindOneAndUpdate(ctx, bson.M{"_id": messageID}, update, opts).Decode(&msg); err != nil {
		return GameMessage{}, fmt.Errorf("record attempt: %w", err)
	}
	return msg, nil
}

func (s *Store) TouchAttempt(ctx context.Context, messageID string) error {
	update := bson.M{
		"$set": bson.M{
			"last_attempt_at": time.Now(),
			"updated_at":      time.Now(),
		},
	}
	res, err := s.collection.UpdateByID(ctx, messageID, update)
	if err != nil {
		return fmt.Errorf("touch attempt: %w", err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("touch attempt: message not found")
	}
	return nil
}

func (s *Store) UpdateMetadata(ctx context.Context, messageID string, meta ImageMeta) error {
	update := bson.M{
		"$set": bson.M{
			"image":      meta,
			"updated_at": time.Now(),
		},
	}
	if _, err := s.collection.UpdateByID(ctx, messageID, update); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

func (s *Store) MarkProcessed(ctx context.Context, messageID string) error {
	update := bson.M{
		"$set": bson.M{
			"status":     statusProcessed,
			"updated_at": time.Now(),
		},
	}
	if _, err := s.collection.UpdateByID(ctx, messageID, update); err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}
	return nil
}

func (s *Store) MoveToFailed(ctx context.Context, messageID string, details map[string]any) error {
	var msg GameMessage
	if err := s.collection.FindOne(ctx, bson.M{"_id": messageID}).Decode(&msg); err != nil {
		return fmt.Errorf("load message for fail: %w", err)
	}

	failedDoc := bson.M{
		"message_id":      msg.ID,
		"status":          msg.Status,
		"attempts":        msg.Attempts,
		"payload":         msg.Payload,
		"image":           msg.Image,
		"last_error":      msg.LastError,
		"first_seen_at":   msg.CreatedAt,
		"last_attempt_at": msg.LastAttemptAt,
		"failed_at":       time.Now(),
		"details":         details,
	}
	if _, err := s.failed.InsertOne(ctx, failedDoc); err != nil {
		return fmt.Errorf("insert failed message: %w", err)
	}
	if _, err := s.collection.DeleteOne(ctx, bson.M{"_id": messageID}); err != nil {
		return fmt.Errorf("delete failed message: %w", err)
	}
	return nil
}

func StatusProcessed() string {
	return statusProcessed
}
