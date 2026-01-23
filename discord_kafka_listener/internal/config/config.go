package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultWriteTimeout         = 5 * time.Second
	defaultBufferSize           = 256
	defaultKafkaClientID        = "discord-kafka-listener"
	defaultHealthAddr           = ":8080"
	defaultTimezone             = "Europe/Moscow"
	defaultEnqueueTimeout       = 1 * time.Second
	defaultKafkaRetryBaseDelay  = 500 * time.Millisecond
	defaultKafkaRetryMaxDelay   = 30 * time.Second
	defaultKafkaRetryMaxPending = 5000
	defaultBackfillPageSize     = 100
	defaultBackfillMaxMessages  = 1000
	defaultBackfillTimeout      = 30 * time.Second
	defaultBackfillMinInterval  = 30 * time.Second
	defaultLastMessageIDPath    = "/var/lib/discord-listener/last_message_id"
)

type Config struct {
	DiscordToken          string
	DiscordChannelID      string
	KafkaBrokers          []string
	KafkaInputTopic       string
	KafkaClientID         string
	KafkaWriteTimeout     time.Duration
	MessageBufferSize     int
	MessageEnqueueTimeout time.Duration
	KafkaRetryBaseDelay   time.Duration
	KafkaRetryMaxDelay    time.Duration
	KafkaRetryMaxAttempts int
	KafkaRetryMaxPending  int
	BackfillPageSize      int
	BackfillMaxMessages   int
	BackfillTimeout       time.Duration
	BackfillMinInterval   time.Duration
	LastMessageIDPath     string
	HealthAddr            string
	Timezone              string
}

func Load() (Config, error) {
	var cfg Config

	var err error
	if cfg.DiscordToken, err = requiredEnv("DISCORD_TOKEN"); err != nil {
		return Config{}, err
	}
	if cfg.DiscordChannelID, err = requiredEnv("DISCORD_CHANNEL_ID"); err != nil {
		return Config{}, err
	}
	brokersRaw, err := requiredEnv("KAFKA_BROKERS")
	if err != nil {
		return Config{}, err
	}
	cfg.KafkaBrokers = splitAndTrim(brokersRaw)
	if len(cfg.KafkaBrokers) == 0 {
		return Config{}, fmt.Errorf("KAFKA_BROKERS must contain at least one broker")
	}
	if cfg.KafkaInputTopic, err = requiredEnv("KAFKA_INPUT_TOPIC"); err != nil {
		return Config{}, err
	}

	cfg.KafkaClientID = optionalEnv("KAFKA_CLIENT_ID", defaultKafkaClientID)
	if cfg.KafkaWriteTimeout, err = durationEnv("KAFKA_WRITE_TIMEOUT", defaultWriteTimeout); err != nil {
		return Config{}, err
	}
	if cfg.MessageBufferSize, err = intEnv("MESSAGE_BUFFER_SIZE", defaultBufferSize); err != nil {
		return Config{}, err
	}
	if cfg.MessageEnqueueTimeout, err = durationEnv("MESSAGE_ENQUEUE_TIMEOUT", defaultEnqueueTimeout); err != nil {
		return Config{}, err
	}
	if cfg.KafkaRetryBaseDelay, err = durationEnv("KAFKA_RETRY_BASE_DELAY", defaultKafkaRetryBaseDelay); err != nil {
		return Config{}, err
	}
	if cfg.KafkaRetryMaxDelay, err = durationEnv("KAFKA_RETRY_MAX_DELAY", defaultKafkaRetryMaxDelay); err != nil {
		return Config{}, err
	}
	if cfg.KafkaRetryMaxAttempts, err = nonNegativeIntEnv("KAFKA_RETRY_MAX_ATTEMPTS", 0); err != nil {
		return Config{}, err
	}
	if cfg.KafkaRetryMaxPending, err = intEnv("KAFKA_RETRY_MAX_PENDING", defaultKafkaRetryMaxPending); err != nil {
		return Config{}, err
	}
	if cfg.BackfillPageSize, err = intEnv("DISCORD_BACKFILL_PAGE_SIZE", defaultBackfillPageSize); err != nil {
		return Config{}, err
	}
	if cfg.BackfillMaxMessages, err = nonNegativeIntEnv("DISCORD_BACKFILL_MAX_MESSAGES", defaultBackfillMaxMessages); err != nil {
		return Config{}, err
	}
	if cfg.BackfillTimeout, err = durationEnv("DISCORD_BACKFILL_TIMEOUT", defaultBackfillTimeout); err != nil {
		return Config{}, err
	}
	if cfg.BackfillMinInterval, err = durationEnv("DISCORD_BACKFILL_MIN_INTERVAL", defaultBackfillMinInterval); err != nil {
		return Config{}, err
	}
	cfg.LastMessageIDPath = optionalEnv("LAST_MESSAGE_ID_PATH", defaultLastMessageIDPath)
	cfg.HealthAddr = optionalEnv("HEALTH_ADDR", defaultHealthAddr)
	cfg.Timezone = optionalEnv("TZ", defaultTimezone)

	return cfg, nil
}

func requiredEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("missing %s", key)
	}
	return value, nil
}

func optionalEnv(key, defaultValue string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	return value
}

func durationEnv(key string, defaultValue time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

func intEnv(key string, defaultValue int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("invalid %s: must be a positive integer", key)
	}
	return parsed, nil
}

func nonNegativeIntEnv(key string, defaultValue int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid %s: must be a non-negative integer", key)
	}
	return parsed, nil
}

func splitAndTrim(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}
