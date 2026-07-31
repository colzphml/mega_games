package processor

import (
	"context"
	"testing"

	"github.com/colzphml/mega_games/internal/common/pgtest"
)

// The image column on game_image_status is JSONB and must be interpreted
// correctly in all four shapes it can hold: unset (SQL NULL), an explicit
// JSON null, a populated object to reuse, and {} — a syntactically valid
// but semantically empty object. {} is not written by the service itself
// (the single writer always fills every field in together), but a manual
// "clear this row for reprocessing" UPDATE could plausibly write '{}'
// instead of NULL, and json.Unmarshal happily turns {} into a non-nil,
// all-zero-value struct — which looks exactly like "image already exists"
// unless something checks for that.
//
// This runs the four states through a real Postgres JSONB column (via the
// pgtest harness) rather than hand-built Go byte literals, so what's under
// test is the actual NULL/'null'::jsonb/'{}'::jsonb round trip, not an
// assumption about it.
func TestDecodeStoredImageMetaAgainstRealJSONB(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewPostgres(t)
	pgtest.ApplySchema(t, pool, `CREATE TABLE image_probe (id TEXT PRIMARY KEY, image JSONB)`)

	insert := func(id, imageExpr string) {
		t.Helper()
		if _, err := pool.Exec(ctx, "INSERT INTO image_probe (id, image) VALUES ($1, "+imageExpr+")", id); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	insert("unset", "NULL")
	insert("json_null", "'null'::jsonb")
	insert("empty_object", "'{}'::jsonb")
	insert("populated", `'{"image_url":"https://example.test/img.png","image_bucket":"game-images","image_object":"games/42/msg-1.png","content_type":"image/png","size":1234,"fetcher":"headless","stored_at":"2026-01-01T00:00:00Z"}'::jsonb`)

	readImage := func(id string) []byte {
		t.Helper()
		var raw []byte
		if err := pool.QueryRow(ctx, `SELECT image FROM image_probe WHERE id = $1`, id).Scan(&raw); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return raw
	}

	cases := []struct {
		name      string
		id        string
		wantReuse bool
	}{
		{"column not filled generates a new image", "unset", false},
		{"JSON null generates a new image", "json_null", false},
		{"empty object generates a new image instead of being silently reused", "empty_object", false},
		{"populated object is reused", "populated", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := readImage(tc.id)
			meta, err := decodeStoredImageMeta(raw)
			if err != nil {
				t.Fatalf("decodeStoredImageMeta(%q): unexpected error: %v", raw, err)
			}

			gotReuse := meta != nil
			if gotReuse != tc.wantReuse {
				t.Errorf("decodeStoredImageMeta(%q) reuse = %v, want %v (meta=%#v)", raw, gotReuse, tc.wantReuse, meta)
			}
			if tc.wantReuse && meta.ObjectKey == "" {
				t.Errorf("expected a populated ObjectKey on a reusable image, got empty")
			}
		})
	}
}
