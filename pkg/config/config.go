// Package config provides a configuration struct for the application.
// It can load configuration values from a YAML file and environment variables.
//
// Usage:
//
//	var cfg config.Config
//	err := cfg.LoadConfig("config.yml")
//	if err != nil {
//	  log.Fatal().Err(err).Msg("failed to load configuration")
//	}
package config

import (
	"fmt"
	"os"

	"github.com/caarlos0/env"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

var log = zerolog.New(os.Stdout).With().Str("package", "config").Timestamp().Logger()

// Config represents the application configuration.
type Config struct {
	App struct {
		Source struct {
			Type      string `yaml:"type" env:"SOURCE_TYPE"`             // Type is the type of the source.
			Token     string `yaml:"token" env:"SOURCE_TOKEN"`           // Token is the token used to authenticate to the source.
			ChannelID string `yaml:"channel_id" env:"SOURCE_CHANNEL_ID"` // ChannelID is the ID of the channel to listen to.
		} `yaml:"source"`
		Target struct {
			Type      string `yaml:"type" env:"TARGET_TYPE"`             // Type is the type of the target.
			Token     string `yaml:"token" env:"TARGET_TOKEN"`           // Token is the token used to authenticate to the target.
			ChannelID string `yaml:"channel_id" env:"TARGET_CHANNEL_ID"` // ChannelID is the ID of the channel to send messages to.
		} `yaml:"target"`
		DeepHistory int `yaml:"deep_history" env:"DEEP_HISTORY"` // DeepHistory is the deep of history message when application start.
	} `yaml:"app"`
}

func (c *Config) LoadConfig(configPath string) (err error) {
	// Read the configuration file.
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	// Unmarshal the configuration data into the Config struct.
	if err = yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	// Parse the environment variables into the Config struct.
	if err = env.Parse(c); err != nil {
		return fmt.Errorf("problem with environment read: %w", err)
	}
	// Validate the configuration variables.
	if err = validateConfig(c); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	return
}

// validateConfig validates the configuration variables.
//
// If any required variable is missing or has an invalid value, this
// function will return an error.
func validateConfig(c *Config) error {
	if c.App.Source.Type == "" {
		return fmt.Errorf("missing SOURCE_TYPE configuration")
	}
	if c.App.Source.Token == "" {
		return fmt.Errorf("missing SOURCE_TOKEN configuration")
	}
	if c.App.Source.ChannelID == "" {
		return fmt.Errorf("missing SOURCE_CHANNEL_ID configuration")
	}
	if c.App.Target.Type == "" {
		return fmt.Errorf("missing TARGET_TYPE configuration")
	}
	if c.App.Target.Token == "" {
		return fmt.Errorf("missing TARGET_TOKEN configuration")
	}
	if c.App.Target.ChannelID == "" {
		return fmt.Errorf("missing TARGET_CHANNEL_ID configuration")
	}
	if c.App.DeepHistory < 1 {
		return fmt.Errorf("DEEP_HISTORY must be greater than 0")
	}
	return nil
}
