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

// Escape escapes Telegram Markdown special characters in s so it can be
// safely interpolated into *bold* or [text](url) text segments.
//
// Do not use Escape on URLs: Telegram expects a raw, unescaped address
// inside the (...) part of a link.
func Escape(s string) string {
	return escaper.Replace(s)
}
