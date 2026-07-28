package tgmarkdown

import "testing"

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
