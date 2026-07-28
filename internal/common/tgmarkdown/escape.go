// Package tgmarkdown escapes text interpolated into Telegram Markdown.
//
// Team names and player nicknames come from the database and go
// straight into *bold* and [link](url) constructs. A nickname like
// some_user made Telegram reject the whole message with HTTP 400, which
// then burned ten retries and landed the post in the failed table.
package tgmarkdown

import "strings"

var escaper = strings.NewReplacer(
	`\`, `\\`,
	"_", `\_`,
	"*", `\*`,
	"[", `\[`,
	"]", `\]`,
	"(", `\(`,
	")", `\)`,
	"~", `\~`,
	"`", "\\`",
	">", `\>`,
	"#", `\#`,
	"+", `\+`,
	"-", `\-`,
	"=", `\=`,
	"|", `\|`,
	"{", `\{`,
	"}", `\}`,
	".", `\.`,
	"!", `\!`,
)

// Escape escapes Telegram MarkdownV2 special characters in s so it can be
// safely interpolated into *bold*, _italic_ or [text](url) text segments.
//
// Do not use Escape on the URL part of a link: MarkdownV2 has a separate,
// narrower escaping rule there. Use EscapeLinkURL instead.
func Escape(s string) string {
	return escaper.Replace(s)
}

// linkURLEscaper implements MarkdownV2's rule for the URL part of an inline
// link: https://core.telegram.org/bots/api#markdownv2-style
//
// Inside (...) only '\' and ')' need escaping. Escaping anything else
// (e.g. '_') would corrupt the address — t.me/some_user must stay exactly
// that, not t.me/some\_user.
var linkURLEscaper = strings.NewReplacer(
	`\`, `\\`,
	")", `\)`,
)

// EscapeLinkURL escapes the URL portion of a MarkdownV2 inline link
// [text](url). It escapes only '\' and ')', per Telegram's rule for link
// destinations — do not use Escape here, its full character set would
// break the URL.
func EscapeLinkURL(s string) string {
	return linkURLEscaper.Replace(s)
}
