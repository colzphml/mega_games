package pgstore

import "time"

type GamePayload struct {
	GameID  string `json:"game_id"`
	GameURL string `json:"game_url"`
}

type ImageMeta struct {
	ImageURL    string    `json:"image_url"`
	Bucket      string    `json:"image_bucket"`
	ObjectKey   string    `json:"image_object"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Fetcher     string    `json:"fetcher"`
	StoredAt    time.Time `json:"stored_at"`
}
