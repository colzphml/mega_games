// Package config reads and validates environment variables.
// It replaces the per-service helpers that had drifted apart:
// intEnv required a positive value in some services and allowed
// zero in others.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func Required(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("missing %s", key)
	}
	return value, nil
}

func Optional(key, def string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	return value
}

func Duration(key string, def time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

func PositiveInt(key string, def int) (int, error) {
	return boundedInt(key, def, 1)
}

func NonNegativeInt(key string, def int) (int, error) {
	return boundedInt(key, def, 0)
}

func boundedInt(key string, def, min int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < min {
		return 0, fmt.Errorf("invalid %s: must be an integer >= %d", key, min)
	}
	return parsed, nil
}

func Bool(key string, def bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s: must be boolean", key)
	}
	return parsed, nil
}

func List(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
