package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultKafkaClientID       = "discord-kafka-game-image"
	defaultKafkaConsumerGroup  = "discord-kafka-game-image"
	defaultKafkaWriteTimeout   = 5 * time.Second
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
	defaultFetcherType         = "headless"
	defaultLeague              = "MEGA"
	defaultBaseURL             = "https://neonsportz.com"
	defaultMinioRegion         = "us-east-1"
	defaultMinioUseSSL         = false
	defaultMinioConnectTimeout = 5 * time.Second
	defaultMinioRetryDelay     = 2 * time.Second
	defaultMinioMaxAttempts    = 30
	defaultMinioUploadTimeout  = 30 * time.Second
	defaultFetchTimeout        = 6 * time.Minute
	defaultGoChromeHeadless    = true
	defaultMinioObjectPrefix   = "game-recaps"
)

type Config struct {
	KafkaBrokers       []string
	KafkaGameTopic     string
	KafkaImageTopic    string
	KafkaConsumerGroup string
	KafkaClientID      string
	KafkaWriteTimeout  time.Duration
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

	MinioEndpoint       string
	MinioAccessKey      string
	MinioSecretKey      string
	MinioBucket         string
	MinioRegion         string
	MinioUseSSL         bool
	MinioConnectTimeout time.Duration
	MinioRetryDelay     time.Duration
	MinioMaxAttempts    int
	MinioPublicURL      string
	MinioObjectPrefix   string
	MinioUploadTimeout  time.Duration

	FetcherType      string
	BaseURL          string
	League           string
	FetchTimeout     time.Duration
	GoChromeHeadless bool

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
	if cfg.KafkaGameTopic, err = requiredEnv("KAFKA_GAME_TOPIC"); err != nil {
		return Config{}, err
	}
	if cfg.KafkaImageTopic, err = requiredEnv("KAFKA_GAME_IMAGE_TOPIC"); err != nil {
		return Config{}, err
	}
	cfg.KafkaConsumerGroup = optionalEnv("KAFKA_GAME_IMAGE_CONSUMER_GROUP", defaultKafkaConsumerGroup)
	cfg.KafkaClientID = optionalEnv("KAFKA_GAME_IMAGE_CLIENT_ID", defaultKafkaClientID)
	if cfg.KafkaWriteTimeout, err = durationEnv("KAFKA_WRITE_TIMEOUT", defaultKafkaWriteTimeout); err != nil {
		return Config{}, err
	}
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

	if cfg.MinioEndpoint, err = requiredEnv("MINIO_ENDPOINT"); err != nil {
		return Config{}, err
	}
	if cfg.MinioAccessKey, err = requiredEnv("MINIO_ACCESS_KEY"); err != nil {
		return Config{}, err
	}
	if cfg.MinioSecretKey, err = requiredEnv("MINIO_SECRET_KEY"); err != nil {
		return Config{}, err
	}
	if cfg.MinioBucket, err = requiredEnv("MINIO_BUCKET"); err != nil {
		return Config{}, err
	}
	cfg.MinioRegion = optionalEnv("MINIO_REGION", defaultMinioRegion)
	if cfg.MinioUseSSL, err = boolEnv("MINIO_USE_SSL", defaultMinioUseSSL); err != nil {
		return Config{}, err
	}
	if cfg.MinioConnectTimeout, err = durationEnv("MINIO_CONNECT_TIMEOUT", defaultMinioConnectTimeout); err != nil {
		return Config{}, err
	}
	if cfg.MinioRetryDelay, err = durationEnv("MINIO_CONNECT_RETRY_DELAY", defaultMinioRetryDelay); err != nil {
		return Config{}, err
	}
	if cfg.MinioMaxAttempts, err = intEnv("MINIO_CONNECT_MAX_ATTEMPTS", defaultMinioMaxAttempts); err != nil {
		return Config{}, err
	}
	cfg.MinioPublicURL = optionalEnv("MINIO_PUBLIC_URL", "")
	cfg.MinioObjectPrefix = optionalEnv("MINIO_OBJECT_PREFIX", defaultMinioObjectPrefix)
	if cfg.MinioUploadTimeout, err = durationEnv("MINIO_UPLOAD_TIMEOUT", defaultMinioUploadTimeout); err != nil {
		return Config{}, err
	}

	cfg.FetcherType = optionalEnv("GAME_IMAGE_FETCHER_TYPE", defaultFetcherType)
	cfg.BaseURL = optionalEnv("NEON_BASE_URL", defaultBaseURL)
	cfg.League = optionalEnv("NEON_LEAGUE", defaultLeague)
	if cfg.FetchTimeout, err = durationEnv("GAME_IMAGE_FETCH_TIMEOUT", defaultFetchTimeout); err != nil {
		return Config{}, err
	}
	if cfg.GoChromeHeadless, err = boolEnv("GOCHROME_HEADLESS", defaultGoChromeHeadless); err != nil {
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

func boolEnv(key string, defaultValue bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s: must be boolean", key)
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
