package tgmarkdown

import (
	"testing"
)

// reservedMarkdownV2Characters is Telegram's own list of MarkdownV2
// special characters (core.telegram.org/bots/api#markdownv2-style),
// transcribed independently of escape.go's escaper table on purpose: if a
// character is ever dropped from that table, this list must still name it
// so the test below still catches the gap. Out of these 19, only six were
// ever exercised by a test before this one (four through Escape directly:
// '_', '*', '[', ']'; two more only indirectly, through EscapeLinkURL's
// narrower rule: '\' and ')'). An unescaped character here makes Telegram
// reject the whole message with HTTP 400, which then burns the retry
// budget and lands the post in the failed table.
var reservedMarkdownV2Characters = []string{
	"\\", "_", "*", "[", "]", "(", ")", "~", "`",
	">", "#", "+", "-", "=", "|", "{", "}", ".", "!",
}

func TestEscapeAllReservedCharacters(t *testing.T) {
	if len(reservedMarkdownV2Characters) != 19 {
		t.Fatalf("test setup error: MarkdownV2 reserves 19 characters, this list has %d", len(reservedMarkdownV2Characters))
	}
	for _, c := range reservedMarkdownV2Characters {
		t.Run(c, func(t *testing.T) {
			got := Escape("a" + c + "b")
			want := "a" + "\\" + c + "b"
			if got != want {
				t.Errorf("Escape(%q) = %q, want %q: %q must be escaped, or Telegram "+
					"rejects the whole message", "a"+c+"b", got, want, c)
			}
		})
	}
}

func TestEscapeUnderscoreInNickname(t *testing.T) {
	got := Escape("some_user")
	if got != `some\_user` {
		t.Errorf("Escape(%q) = %q, want %q: an unescaped underscore makes "+
			"Telegram reject the whole message with HTTP 400", "some_user", got, `some\_user`)
	}
}

func TestEscapeTeamNameWithAsterisk(t *testing.T) {
	got := Escape("49*ers")
	if got != `49\*ers` {
		t.Errorf("Escape(%q) = %q, want %q", "49*ers", got, `49\*ers`)
	}
}

func TestEscapeLeavesPlainTextAlone(t *testing.T) {
	if got := Escape("Falcons"); got != "Falcons" {
		t.Errorf("Escape(%q) = %q, want unchanged", "Falcons", got)
	}
}

func TestEscapeBrackets(t *testing.T) {
	if got := Escape("a[b]c"); got != `a\[b\]c` {
		t.Errorf("Escape(%q) = %q", "a[b]c", got)
	}
}

func TestEscapeLinkURLLeavesUnderscoreRaw(t *testing.T) {
	got := EscapeLinkURL("t.me/some_user")
	if got != "t.me/some_user" {
		t.Errorf("EscapeLinkURL(%q) = %q, want unchanged: escaping '_' would break the URL", "t.me/some_user", got)
	}
}

func TestEscapeLinkURLEscapesCloseParenAndBackslash(t *testing.T) {
	got := EscapeLinkURL(`t.me/a)b\c`)
	want := `t.me/a\)b\\c`
	if got != want {
		t.Errorf("EscapeLinkURL(%q) = %q, want %q", `t.me/a)b\c`, got, want)
	}
}
