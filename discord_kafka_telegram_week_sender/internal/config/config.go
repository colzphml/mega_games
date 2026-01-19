package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultKafkaConsumerGroup  = "discord-kafka-telegram-week-sender"
	defaultKafkaClientID       = "discord-kafka-telegram-week-sender"
	defaultKafkaReadTimeout    = 10 * time.Second
	defaultHealthAddr          = ":8080"
	defaultTimezone            = "Europe/Moscow"
	defaultPostgresPort        = 5432
	defaultPostgresSSLMode     = "disable"
	defaultPostgresTimeout     = 5 * time.Second
	defaultPostgresRetryDelay  = 2 * time.Second
	defaultPostgresMaxAttempts = 30
	defaultMaxAttempts         = 10
	defaultRetryInterval       = 1 * time.Minute
)

type Config struct {
	KafkaBrokers       []string
	KafkaTopic         string
	KafkaConsumerGroup string
	KafkaClientID      string
	KafkaReadTimeout   time.Duration

	PostgresHost        string
	PostgresPort        int
	PostgresUser        string
	PostgresPass        string
	PostgresDB          string
	PostgresSSLMode     string
	PostgresTimeout     time.Duration
	PostgresRetryDelay  time.Duration
	PostgresMaxAttempts int

	TelegramToken  string
	TelegramChatID string

	HealthAddr           string
	Timezone             string
	MaxAttempts          int
	ProcessRetryInterval time.Duration
}

func Load() (Config, error) {
	var cfg Config
	var err error

	brokersRaw, err := requiredEnv("KAFKA_BROKERS")
	if err != nil {
		return Config{}, err
	}
	cfg.KafkaBrokers = splitAndTrim(brokersRaw)
	if len(cfg.KafkaBrokers) == 0 {
		return Config{}, fmt.Errorf("KAFKA_BROKERS must contain at least one broker")
	}
	if cfg.KafkaTopic, err = requiredEnv("KAFKA_TELEGRAM_WEEK_TOPIC"); err != nil {
		return Config{}, err
	}
	cfg.KafkaConsumerGroup = optionalEnv("KAFKA_TELEGRAM_WEEK_SENDER_GROUP", defaultKafkaConsumerGroup)
	cfg.KafkaClientID = optionalEnv("KAFKA_TELEGRAM_WEEK_SENDER_CLIENT_ID", defaultKafkaClientID)
	if cfg.KafkaReadTimeout, err = durationEnv("KAFKA_READ_TIMEOUT", defaultKafkaReadTimeout); err != nil {
		return Config{}, err
	}

	if cfg.PostgresHost, err = requiredEnv("POSTGRES_HOST"); err != nil {
		return Config{}, err
	}
	if cfg.PostgresUser, err = requiredEnv("POSTGRES_USER"); err != nil {
		return Config{}, err
	}
	if cfg.PostgresPass, err = requiredEnv("POSTGRES_PASSWORD"); err != nil {
		return Config{}, err
	}
	if cfg.PostgresDB, err = requiredEnv("POSTGRES_DB"); err != nil {
		return Config{}, err
	}
	if cfg.PostgresPort, err = intEnv("POSTGRES_PORT", defaultPostgresPort); err != nil {
		return Config{}, err
	}
	cfg.PostgresSSLMode = optionalEnv("POSTGRES_SSLMODE", defaultPostgresSSLMode)
	if cfg.PostgresTimeout, err = durationEnv("POSTGRES_CONNECT_TIMEOUT", defaultPostgresTimeout); err != nil {
		return Config{}, err
	}
	if cfg.PostgresRetryDelay, err = durationEnv("POSTGRES_CONNECT_RETRY_DELAY", defaultPostgresRetryDelay); err != nil {
		return Config{}, err
	}
	if cfg.PostgresMaxAttempts, err = intEnv("POSTGRES_CONNECT_MAX_ATTEMPTS", defaultPostgresMaxAttempts); err != nil {
		return Config{}, err
	}

	if cfg.TelegramToken, err = requiredEnv("TELEGRAM_BOT_TOKEN"); err != nil {
		return Config{}, err
	}
	if cfg.TelegramChatID, err = requiredEnv("TELEGRAM_WEEK_CHAT_ID"); err != nil {
		return Config{}, err
	}

	cfg.HealthAddr = optionalEnv("HEALTH_ADDR", defaultHealthAddr)
	cfg.Timezone = optionalEnv("TZ", defaultTimezone)
	if cfg.MaxAttempts, err = intEnv("PROCESS_MAX_ATTEMPTS", defaultMaxAttempts); err != nil {
		return Config{}, err
	}
	if cfg.ProcessRetryInterval, err = durationEnv("PROCESS_RETRY_INTERVAL", defaultRetryInterval); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s", c.PostgresUser, c.PostgresPass, c.PostgresHost, c.PostgresPort, c.PostgresDB, c.PostgresSSLMode)
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
