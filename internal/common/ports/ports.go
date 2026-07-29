// Package ports declares the boundaries between this system and the
// services it talks to. Concrete clients satisfy these interfaces; tests
// substitute fakes.
//
// Before this package the processors took *discord.Client and
// *telegram.Client directly, so there was nothing to substitute and the
// processing logic could not be tested at all.
package ports

import (
	"context"
	"net/http"

	"github.com/bwmarrin/discordgo"
)

type DiscordMessages interface {
	FetchMessage(ctx context.Context, id string) (*discordgo.Message, error)
}

type WeekSender interface {
	SendWeekMessage(ctx context.Context, text string) error
}

type GameSender interface {
	SendGameImage(ctx context.Context, caption string, image []byte) error
}

type ObjectDownloader interface {
	Download(ctx context.Context, bucket, key string) ([]byte, error)
}

// HTTPDoer is satisfied by *http.Client. Injecting it lets tests point
// the NeonSportz calls at an httptest server.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}
