package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rs/zerolog"

	"github.com/colzphml/mega_games/discord_kafka_telegram_game_sender/internal/config"
)

type Client struct {
	client *minio.Client
	log    zerolog.Logger
}

func New(cfg config.Config, log zerolog.Logger) (*Client, error) {
	endpoint := strings.TrimSpace(cfg.MinioEndpoint)
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
		Region: cfg.MinioRegion,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	return &Client{client: minioClient, log: log}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.client.ListBuckets(ctx)
	if err != nil {
		return fmt.Errorf("minio ping: %w", err)
	}
	return nil
}

func (c *Client) Download(ctx context.Context, bucket, objectKey string) ([]byte, error) {
	objectKey = strings.TrimSpace(objectKey)
	bucket = strings.TrimSpace(bucket)
	if bucket == "" || objectKey == "" {
		return nil, fmt.Errorf("bucket or object key is empty")
	}
	reader, err := c.client.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			c.log.Error().Err(closeErr).Msg("failed to close minio reader")
		}
	}()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}
	return data, nil
}

func (c *Client) WaitForReady(ctx context.Context, cfg config.Config) (*Client, error) {
	timeout := cfg.MinioTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	for attempt := 1; attempt <= cfg.MinioMaxAttempts; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, timeout)
		err := c.Ping(pingCtx)
		cancel()
		if err == nil {
			return c, nil
		}
		if attempt == cfg.MinioMaxAttempts {
			return nil, err
		}
		c.log.Warn().Err(err).Int("attempt", attempt).Int("max_attempts", cfg.MinioMaxAttempts).Msg("minio not ready, retrying")
		if !sleepWithContext(ctx, cfg.MinioRetryDelay) {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("minio not ready")
}

func sleepWithContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
