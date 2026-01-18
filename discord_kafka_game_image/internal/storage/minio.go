package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rs/zerolog"

	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/config"
)

type Client struct {
	client        *minio.Client
	bucket        string
	publicBaseURL string
	objectPrefix  string
	useSSL        bool
	endpoint      string
	log           zerolog.Logger
}

type UploadResult struct {
	Bucket      string
	ObjectKey   string
	Size        int64
	ContentType string
	URL         string
}

func New(ctx context.Context, cfg config.Config, log zerolog.Logger) (*Client, error) {
	endpoint, scheme := normalizeEndpoint(cfg.MinioEndpoint)
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
		Region: cfg.MinioRegion,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	publicURL := strings.TrimSpace(cfg.MinioPublicURL)
	if publicURL == "" {
		publicURL = fmt.Sprintf("%s://%s", schemeForURL(cfg.MinioUseSSL, scheme), endpoint)
	}

	client := &Client{
		client:        minioClient,
		bucket:        cfg.MinioBucket,
		publicBaseURL: strings.TrimRight(publicURL, "/"),
		objectPrefix:  strings.Trim(cfg.MinioObjectPrefix, "/"),
		log:           log,
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.MinioConnectTimeout)
	defer cancel()
	if err := client.ensureBucket(pingCtx, cfg.MinioRegion); err != nil {
		return nil, err
	}

	return client, nil
}

func (c *Client) Upload(ctx context.Context, objectKey, contentType string, payload []byte) (UploadResult, error) {
	reader := bytes.NewReader(payload)
	info, err := c.client.PutObject(ctx, c.bucket, objectKey, reader, int64(len(payload)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return UploadResult{}, fmt.Errorf("upload to minio: %w", err)
	}

	return UploadResult{
		Bucket:      c.bucket,
		ObjectKey:   objectKey,
		Size:        info.Size,
		ContentType: contentType,
		URL:         c.objectURL(objectKey),
	}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.client.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("minio ping: %w", err)
	}
	return nil
}

func (c *Client) ObjectKey(gameID, messageID, ext string) string {
	safeMessage := strings.ReplaceAll(messageID, ":", "_")
	safeGame := strings.TrimSpace(gameID)
	fileName := fmt.Sprintf("%s%s", safeMessage, ext)
	if safeGame != "" {
		fileName = path.Join(safeGame, fileName)
	}
	if c.objectPrefix == "" {
		return path.Clean(fileName)
	}
	return path.Clean(path.Join(c.objectPrefix, fileName))
}

func (c *Client) ensureBucket(ctx context.Context, region string) error {
	exists, err := c.client.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("check minio bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := c.client.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		return fmt.Errorf("create minio bucket: %w", err)
	}
	c.log.Info().Str("bucket", c.bucket).Msg("created minio bucket")
	return nil
}

func (c *Client) objectURL(objectKey string) string {
	base := strings.TrimRight(c.publicBaseURL, "/")
	return fmt.Sprintf("%s/%s/%s", base, c.bucket, strings.TrimLeft(objectKey, "/"))
}

func normalizeEndpoint(endpoint string) (string, string) {
	clean := strings.TrimSpace(endpoint)
	if clean == "" {
		return "", "http"
	}
	if strings.HasPrefix(clean, "http://") || strings.HasPrefix(clean, "https://") {
		parsed, err := url.Parse(clean)
		if err == nil && parsed.Host != "" {
			return parsed.Host, parsed.Scheme
		}
	}
	return clean, "http"
}

func schemeForURL(secure bool, fallback string) string {
	if secure {
		return "https"
	}
	if fallback != "" {
		return fallback
	}
	return "http"
}
