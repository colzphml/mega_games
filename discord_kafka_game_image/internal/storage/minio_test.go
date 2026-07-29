package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/config"
)

// TestObjectKey checks key construction against real inputs, not
// container-backed: the shape of the key does not depend on MinIO
// being up, so this runs even under -short.
func TestObjectKey(t *testing.T) {
	client := &Client{objectPrefix: "game-recaps"}

	tests := []struct {
		name      string
		gameID    string
		messageID string
		ext       string
		want      string
	}{
		{
			name: "real composite message id loses its colon",
			// Discord message id and game id, joined by the producer
			// side exactly as they arrive over Kafka.
			gameID:    "26912152",
			messageID: "1531337639100158009:26912152",
			ext:       ".png",
			want:      "game-recaps/26912152/1531337639100158009_26912152.png",
		},
		{
			name:      "missing game id skips the game directory",
			gameID:    "",
			messageID: "1531337639100158009",
			ext:       ".png",
			want:      "game-recaps/1531337639100158009.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.ObjectKey(tt.gameID, tt.messageID, tt.ext)
			if got != tt.want {
				t.Errorf("ObjectKey(%q, %q, %q) = %q, want %q",
					tt.gameID, tt.messageID, tt.ext, got, tt.want)
			}
			if bytes.Contains([]byte(got), []byte(":")) {
				t.Errorf("object key %q must not contain a colon: S3 keys with "+
					"colons break direct links in some clients", got)
			}
		})
	}
}

// TestUploadAndObjectKey uploads a real payload to a real MinIO
// container through the same client the service uses, then confirms
// both the key it was stored under and the bytes MinIO actually
// persisted.
func TestUploadAndObjectKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}
	endpoint, access, secret := startMinio(t)

	cfg := config.Config{
		MinioEndpoint:       endpoint,
		MinioAccessKey:      access,
		MinioSecretKey:      secret,
		MinioBucket:         "test-bucket",
		MinioRegion:         "us-east-1",
		MinioObjectPrefix:   "game-recaps",
		MinioConnectTimeout: 10 * time.Second,
	}

	client, err := New(context.Background(), cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	payload := []byte("fake png bytes")
	// The id Discord and the game processor actually produce: message
	// id and game id joined by a colon.
	key := client.ObjectKey("26912152", "1531337639100158009:26912152", ".png")

	got, err := client.Upload(context.Background(), key, "image/png", payload)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if got.Size != int64(len(payload)) {
		t.Errorf("size = %d, want %d", got.Size, len(payload))
	}
	if !bytes.Contains([]byte(got.ObjectKey), []byte("26912152")) {
		t.Errorf("object key %q must contain the game id", got.ObjectKey)
	}
	if bytes.Contains([]byte(got.ObjectKey), []byte(":")) {
		t.Errorf("object key %q must not contain a colon: S3 keys with colons "+
			"break direct links in some clients", got.ObjectKey)
	}

	stored := getObject(t, endpoint, access, secret, cfg.MinioBucket, got.ObjectKey)
	if !bytes.Equal(stored, payload) {
		t.Errorf("stored payload = %q, want %q: PutObject's reported size is not "+
			"proof the bytes MinIO kept match what was sent", stored, payload)
	}
}

func startMinio(t *testing.T) (endpoint, accessKey, secretKey string) {
	t.Helper()

	ctx := context.Background()
	container, err := tcminio.Run(ctx, "minio/minio:RELEASE.2024-09-13T20-26-02Z")
	if err != nil {
		t.Fatalf("start minio: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate minio: %v", err)
		}
	})

	host, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return host, container.Username, container.Password
}

// getObject fetches an object straight through the minio-go SDK,
// independent of storage.Client, so the assertion does not just trust
// the same code path that produced it.
func getObject(t *testing.T, endpoint, accessKey, secretKey, bucket, key string) []byte {
	t.Helper()

	raw, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("create raw minio client: %v", err)
	}

	obj, err := raw.GetObject(context.Background(), bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("get object: %v", err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	return data
}
