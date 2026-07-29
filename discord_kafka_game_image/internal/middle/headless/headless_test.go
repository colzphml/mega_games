package headless

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseColorNormal(t *testing.T) {
	// 0xFF0000 = 16711680
	got := parseColor("16711680")
	want := color.RGBA{255, 0, 0, 255}
	if got != want {
		t.Errorf("parseColor(16711680) = %v, want %v", got, want)
	}
}

func TestParseColorOverflow(t *testing.T) {
	// 0x1000000 needs seven hex digits; slicing [0:2] used to read "10"
	// and produce a wrong colour instead of clamping.
	got := parseColor("16777216")
	if got.A != 255 {
		t.Errorf("alpha = %d, want 255", got.A)
	}
	if got.R == 0x10 {
		t.Error("seven-digit hex must not be sliced as if it were six")
	}
}

func TestParseColorEmpty(t *testing.T) {
	got := parseColor("")
	want := color.RGBA{0, 0, 0, 255}
	if got != want {
		t.Errorf("parseColor(\"\") = %v, want opaque black", got)
	}
}

func TestParseColorGarbage(t *testing.T) {
	got := parseColor("not-a-number")
	if got.A != 255 {
		t.Errorf("alpha = %d, want 255 even for unparseable input", got.A)
	}
}

// buildRecapImage does not (yet) accept an injected HTTP client — loadImage
// dials out with its own http.Client internally. These tests therefore
// exercise the real behaviour through a httptest.Server URL passed as
// baseURL, rather than swapping in srv.Client().
func TestBuildRecapImageSurvivesMissingLogo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Логотипы недоступны, фон отдаётся.
		if strings.Contains(r.URL.Path, "teamlogos") || strings.Contains(r.URL.Path, "logo.png") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, image.NewRGBA(image.Rect(0, 0, 64, 64)))
	}))
	defer srv.Close()

	rec := Recap{}
	rec.Game.HomeTeam = Team{DisplayName: "Falcons", PrimaryColor: "16711680"}
	rec.Game.AwayTeam = Team{DisplayName: "Bucs", PrimaryColor: "255"}

	img, err := buildRecapImage(context.Background(), srv.URL, 5*time.Second, rec)
	if err != nil {
		t.Fatalf("a missing logo must not fail the whole image: %v", err)
	}
	if img == nil {
		t.Fatal("image must be produced")
	}
}

func TestBuildRecapImageFailsWithoutBackground(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	rec := Recap{}
	if _, err := buildRecapImage(context.Background(), srv.URL, 5*time.Second, rec); err == nil {
		t.Error("without a background there is nothing to draw on; this must fail")
	}
}
