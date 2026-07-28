package headless

import (
	"image/color"
	"testing"
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
