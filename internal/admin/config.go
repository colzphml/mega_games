package admin

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHealthAddr = ":8081"
	defaultPGPort     = 5432
	defaultTimezone   = "Europe/Moscow"
)

type Config struct {
	HealthAddr       string
	PostgresHost     string
	PostgresPort     int
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	Timezone         string
}

func Load() (Config, error) {
	var cfg Config
	var err error

	cfg.HealthAddr = optionalEnv("HEALTH_ADDR", defaultHealthAddr)

	if cfg.PostgresHost, err = requiredEnv("POSTGRES_HOST"); err != nil {
		return Config{}, err
	}
	if cfg.PostgresPort, err = intEnv("POSTGRES_PORT", defaultPGPort); err != nil {
		return Config{}, err
	}
	if cfg.PostgresUser, err = requiredEnv("POSTGRES_USER"); err != nil {
		return Config{}, err
	}
	if cfg.PostgresPassword, err = requiredEnv("POSTGRES_PASSWORD"); err != nil {
		return Config{}, err
	}
	if cfg.PostgresDB, err = requiredEnv("POSTGRES_DB"); err != nil {
		return Config{}, err
	}

	cfg.Timezone = optionalEnv("TZ", defaultTimezone)

	return cfg, nil
}

func (c Config) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		c.PostgresUser,
		c.PostgresPassword,
		c.PostgresHost,
		c.PostgresPort,
		c.PostgresDB,
	)
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

func intEnv(key string, defaultValue int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: must be an integer", key)
	}
	return parsed, nil
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
