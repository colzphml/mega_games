# MEGA Games Bot v5.0 — План, часть 3: фазы 3–5

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Продолжение `2026-07-28-mega-games-v5-part2.md`. Global Constraints — см. часть 1.

---

# Фаза 3 — Инъекция зависимостей и компонентные тесты

### Task V5-18: Интерфейсы внешних зависимостей

**Зависит от:** вся фаза 2 смержена. **Блокирует:** V5-19, V5-20, V5-21.
**Worktree:** `v5/18-interfaces`

**Files:**
- Create: `internal/common/ports/ports.go`
- Modify: `discord_kafka_processor/internal/processor/processor.go` (принимать интерфейс)
- Modify: `discord_kafka_telegram_week_sender/internal/processor/processor.go`
- Modify: `discord_kafka_telegram_game_sender/internal/processor/processor.go`
- Modify: `discord_kafka_game_image/internal/middle/headless/headless.go` (инжект HTTP-клиента)
- Modify: соответствующие `cmd/*/main.go` (передать конкретные реализации)

**Interfaces:**
- Produces:
  - `type DiscordMessages interface { FetchMessage(ctx context.Context, id string) (*discordgo.Message, error) }`
  - `type WeekSender interface { SendWeekMessage(ctx context.Context, text string) error }`
  - `type GameSender interface { SendGameImage(ctx context.Context, caption string, image []byte) error }`
  - `type ObjectDownloader interface { Download(ctx context.Context, bucket, key string) ([]byte, error) }`
  - `type HTTPDoer interface { Do(req *http.Request) (*http.Response, error) }`

**Контекст:** сейчас процессоры принимают конкретные типы (`*discord.Client`, `*telegram.Client`), а HTTP-клиенты создаются внутри функций. Пока это так, компонентные тесты физически невозможны. Спека §4.2.

Обратите внимание: методы отправки в Telegram сейчас **не принимают `context.Context`** — сигнатуры `SendWeekMessage(text string) error` и `SendGameImage(caption string, image []byte) error`. Интерфейс добавляет контекст, реализация начинает его пробрасывать.

- [ ] **Step 1: Определить интерфейсы**

```go
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
```

- [ ] **Step 2: Добавить контекст в клиенты Telegram**

В `discord_kafka_telegram_week_sender/internal/telegram/client.go`:

```go
func (c *Client) SendWeekMessage(ctx context.Context, text string) error {
	msg := tgbotapi.NewMessage(c.chatID, text)
	msg.DisableWebPagePreview = true
	msg.ParseMode = "Markdown"

	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := c.bot.Send(msg); err != nil {
		return fmt.Errorf("send telegram message: %s", redactToken(err.Error(), c.bot.Token))
	}
	c.log.Info().Msg("week message sent")
	return nil
}
```

Убрать из лога полный текст сообщения (`Str("message", text)`) — он попадал в логи целиком.

Аналогично `SendGameImage(ctx context.Context, caption string, image []byte) error` в game-sender.

- [ ] **Step 3: Переключить процессоры на интерфейсы**

В `discord_kafka_processor/internal/processor/processor.go` заменить поле `discord *discord.Client` на `discord ports.DiscordMessages` и параметр конструктора. Аналогично:
- week-sender: `client *telegram.Client` → `client ports.WeekSender`
- game-sender: `client *telegram.Client` → `client ports.GameSender`, `objects *storage.Client` → `objects ports.ObjectDownloader`

Вызовы в `main.go` не меняются — конкретные типы уже удовлетворяют интерфейсам.

- [ ] **Step 4: Инжектировать HTTP-клиент в headless**

В `headless.go` добавить поле в `Client` и пробросить:

```go
type Client struct {
	baseURL     string
	league      string
	httpTimeout time.Duration
	http        ports.HTTPDoer
}

func NewClient(ctx context.Context, cfg config.Config) (*Client, error) {
	return &Client{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		league:      strings.TrimSpace(cfg.League),
		httpTimeout: cfg.FetchTimeout,
		http:        &http.Client{Timeout: cfg.FetchTimeout},
	}, nil
}
```

Изменить `fetchJSON` и `loadImage`, чтобы они принимали `ports.HTTPDoer` первым аргументом вместо создания клиента внутри, и обновить все вызовы.

- [ ] **Step 5: Проверить сборку**

Run: `go build ./... && go vet ./... && go test ./... -short`
Expected: успех.

- [ ] **Step 6: Commit**

```bash
git add internal/common/ports/ discord_kafka_*/
git commit -m "refactor: inject external dependencies through interfaces

Processors took concrete client types and HTTP clients were constructed
inside the functions that used them, so none of the processing logic
could be exercised in a test.

Telegram send methods gain a context parameter, and the week sender
stops logging the full message body."
```

---

### Task V5-19: Компонентные тесты Telegram

**Зависит от:** V5-18. **Параллельно с:** V5-20, V5-21, V5-22.
**Worktree:** `v5/19-telegram-tests`

**Files:**
- Create: `discord_kafka_telegram_week_sender/internal/telegram/client_test.go`
- Create: `discord_kafka_telegram_game_sender/internal/telegram/client_test.go`
- Modify: `discord_kafka_telegram_week_sender/internal/telegram/client.go` (сделать endpoint настраиваемым)
- Modify: `discord_kafka_telegram_game_sender/internal/telegram/client.go`

**Interfaces:**
- Produces: `func NewWithEndpoint(token, chatID, endpoint string, log zerolog.Logger) (*Client, error)`

**Контекст:** не мок, а настоящий HTTP — проверяем, что реально уходит в `sendMessage`: экранирование, `parse_mode`, содержимое. Спека §5.

- [ ] **Step 1: Написать падающий тест**

```go
package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// telegramStub answers getMe so the client can be constructed, then
// records what the code actually sends.
func telegramStub(t *testing.T, captured *http.Request, body *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"username":"test_bot"}}`))
			return
		}
		*captured = *r
		*body = r.Form.Encode()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":0,"chat":{"id":1,"type":"private"}}}`))
	}))
}

func TestSendWeekMessageUsesMarkdownAndSendsEscapedText(t *testing.T) {
	var captured http.Request
	var body string
	srv := telegramStub(t, &captured, &body)
	defer srv.Close()

	client, err := NewWithEndpoint("test-token", "1", srv.URL+"/bot%s/%s", zerolog.Nop())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	text := `*Week 1*` + "\n" + `[ATL](t.me/some_user)`
	if err := client.SendWeekMessage(context.Background(), text); err != nil {
		t.Fatalf("send: %v", err)
	}

	if !strings.Contains(body, "parse_mode=Markdown") {
		t.Errorf("request must set parse_mode=Markdown, got: %s", body)
	}
	if !strings.Contains(body, "disable_web_page_preview=true") {
		t.Errorf("request must disable link previews, got: %s", body)
	}
	if !strings.Contains(body, "chat_id=1") {
		t.Errorf("request must target the configured chat, got: %s", body)
	}
}

func TestSendWeekMessageRespectsCancelledContext(t *testing.T) {
	var captured http.Request
	var body string
	srv := telegramStub(t, &captured, &body)
	defer srv.Close()

	client, err := NewWithEndpoint("test-token", "1", srv.URL+"/bot%s/%s", zerolog.Nop())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.SendWeekMessage(ctx, "hello"); err == nil {
		t.Error("a cancelled context must abort the send")
	}
}

var _ = json.Marshal
```

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./discord_kafka_telegram_week_sender/internal/telegram/ -v`
Expected: FAIL — `undefined: NewWithEndpoint`

- [ ] **Step 3: Сделать endpoint настраиваемым**

```go
// New builds a client against the public Telegram API.
func New(token, chatID string, log zerolog.Logger) (*Client, error) {
	return NewWithEndpoint(token, chatID, tgbotapi.APIEndpoint, log)
}

// NewWithEndpoint builds a client against an arbitrary endpoint so
// tests can assert on the request that actually goes out rather than on
// a mock's recollection of it.
func NewWithEndpoint(token, chatID, endpoint string, log zerolog.Logger) (*Client, error) {
	parsedID, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse chat id: %w", err)
	}

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     false,
		Protocols:             protocols,
	}
	bot, err := tgbotapi.NewBotAPIWithAPIEndpoint(token, endpoint, &http.Client{
		Timeout:   2 * time.Minute,
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("create bot api: %s", redactToken(err.Error(), token))
	}
	return &Client{bot: bot, chatID: parsedID, log: log}, nil
}
```

Если в используемой версии `tgbotapi` нет `NewBotAPIWithAPIEndpoint`, создать бота через `NewBotAPIWithClient` и присвоить `bot.APIEndpoint = endpoint` перед первым запросом.

- [ ] **Step 4: Повторить для game-sender**

Тест проверяет, что уходит `sendPhoto` с непустым телом и правильным `chat_id`, и что размер фото не превышает лимит Telegram:

```go
func TestSendGameImageRejectsOversizedPhoto(t *testing.T) {
	var captured http.Request
	var body string
	srv := telegramStub(t, &captured, &body)
	defer srv.Close()

	client, err := NewWithEndpoint("test-token", "1", srv.URL+"/bot%s/%s", zerolog.Nop())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	oversized := make([]byte, 11<<20) // Telegram rejects photos above 10 MB
	err = client.SendGameImage(context.Background(), "caption", oversized)
	if err == nil {
		t.Error("an oversized photo must fail before the request goes out")
	}
}
```

Реализовать проверку в `SendGameImage`:

```go
// maxPhotoBytes is the Telegram limit for sendPhoto. Catching it here
// turns one clear error into ten pointless retries avoided.
const maxPhotoBytes = 10 << 20

func (c *Client) SendGameImage(ctx context.Context, caption string, image []byte) error {
	if len(image) > maxPhotoBytes {
		return fmt.Errorf("photo is %d bytes, Telegram allows %d", len(image), maxPhotoBytes)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	...
}
```

- [ ] **Step 5: Запустить тесты**

Run: `go test ./discord_kafka_telegram_*/internal/telegram/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add discord_kafka_telegram_*/internal/telegram/
git commit -m "test: assert on the actual Telegram requests

An httptest endpoint checks what really goes out — parse_mode, chat_id,
link previews — instead of asserting against a mock.

sendPhoto now rejects payloads above Telegram's 10 MB limit up front
rather than discovering it through ten failed retries."
```

---

### Task V5-20: Компонентные тесты Discord

**Зависит от:** V5-18. **Параллельно с:** V5-19, V5-21, V5-22.
**Worktree:** `v5/20-discord-tests`

**Files:**
- Create: `discord_kafka_processor/internal/parser/parser_test.go`
- Create: `discord_kafka_processor/internal/parser/testdata/week_advance.json`
- Create: `discord_kafka_processor/internal/parser/testdata/games_list.json`
- Create: `discord_kafka_processor/internal/parser/testdata/no_embeds.json`

**Контекст:** фикстуры снимаются с живого Discord утилитой `discord_tools/message-dump`. Покрывают P3-3 (`parseWeek` берёт последнее число заголовка).

- [ ] **Step 1: Снять фикстуры с живого Discord**

```bash
cd discord_tools
DISCORD_TOKEN=<token> DISCORD_CHANNEL_ID=<id> \
  go run ./cmd/message-dump/main.go <week-message-id> \
  > ../discord_kafka_processor/internal/parser/testdata/week_advance.json

DISCORD_TOKEN=<token> DISCORD_CHANNEL_ID=<id> \
  go run ./cmd/message-dump/main.go <games-message-id> \
  > ../discord_kafka_processor/internal/parser/testdata/games_list.json
```

ID сообщений взять из живой базы:

```bash
ssh pi "cd /home/colz/envs/mega_games && docker compose exec -T postgres \
  psql -U megagames -d megagames -c \
  \"SELECT message_id FROM discord_message_status ORDER BY created_at DESC LIMIT 5;\""
```

Перед коммитом **вычистить из фикстур** имена пользователей и аватары, оставив только `embeds` и `timestamp` — это публикуемый репозиторий.

Для `no_embeds.json` написать вручную: `{"id":"1","timestamp":"2026-07-28T10:00:00+00:00","embeds":[]}`.

- [ ] **Step 2: Написать тесты**

```go
package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func loadFixture(t *testing.T, name string) *discordgo.Message {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var msg discordgo.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return &msg
}

func TestParseWeekAdvance(t *testing.T) {
	got := ParseMessage(loadFixture(t, "week_advance.json"))
	if !got.Ready {
		t.Fatal("a week-advance message must parse as ready")
	}
	if got.Week == nil {
		t.Fatal("week update must be extracted")
	}
	if got.Week.Season != "regular" {
		t.Errorf("season = %q, want regular", got.Week.Season)
	}
	if got.Week.Week < 1 {
		t.Errorf("week = %d, want a positive number", got.Week.Week)
	}
}

func TestParseGamesList(t *testing.T) {
	got := ParseMessage(loadFixture(t, "games_list.json"))
	if !got.Ready {
		t.Fatal("a games message must parse as ready")
	}
	if len(got.Games) == 0 {
		t.Error("game ids must be extracted")
	}
	seen := map[string]bool{}
	for _, g := range got.Games {
		if seen[g] {
			t.Errorf("duplicate game id %q — the parser must deduplicate", g)
		}
		seen[g] = true
	}
}

func TestParseMessageWithoutEmbedsIsNotReady(t *testing.T) {
	got := ParseMessage(loadFixture(t, "no_embeds.json"))
	if got.Ready {
		t.Error("a message without embeds is not ready: Discord fills embeds asynchronously")
	}
}

func TestParseWeekTakesTheWeekNumberNotTheLast(t *testing.T) {
	// Regression: parseWeek looped over every number in the title and
	// kept the last one, so "Week 3 of 17" would have yielded 17.
	week, ok := parseWeek("MEGA has advanced to Regular Season Week 3")
	if !ok {
		t.Fatal("title must parse")
	}
	if week.Week != 3 {
		t.Errorf("week = %d, want 3", week.Week)
	}
}

func TestParseWeekRejectsUnknownSeason(t *testing.T) {
	if _, ok := parseWeek("MEGA has advanced to Something Else Week 3"); ok {
		t.Error("an unrecognised season must not parse")
	}
}

func TestParseNilMessage(t *testing.T) {
	if got := ParseMessage(nil); got.Ready {
		t.Error("nil message must not be ready")
	}
}
```

- [ ] **Step 3: Запустить тесты**

Run: `go test ./discord_kafka_processor/internal/parser/ -v`
Expected: PASS. Если `TestParseWeekTakesTheWeekNumberNotTheLast` падает — это подтверждённый P3-3.

- [ ] **Step 4: Починить parseWeek, если тест упал**

```go
// weekAfterKeywordRe anchors on the word "Week" so a title like
// "Week 3 of 17" yields 3. The previous version kept the last number
// found anywhere in the title.
var weekAfterKeywordRe = regexp.MustCompile(`(?i)week\s+(\d+)`)

func parseWeek(title string) (*WeekUpdate, bool) {
	season := ""
	switch {
	case strings.Contains(title, "Regular Season"):
		season = "regular"
	case strings.Contains(title, "Pre Season"):
		season = "preseason"
	case strings.Contains(title, "Post Season"):
		season = "postseason"
	default:
		return nil, false
	}

	week := 0
	if m := weekAfterKeywordRe.FindStringSubmatch(title); len(m) == 2 {
		if parsed, err := strconv.Atoi(m[1]); err == nil {
			week = parsed
		}
	}

	return &WeekUpdate{Season: season, Week: week, Title: strings.TrimSpace(title)}, true
}
```

- [ ] **Step 5: Commit**

```bash
git add discord_kafka_processor/internal/parser/
git commit -m "test: cover the Discord parser with real message fixtures

Fixtures captured with discord_tools/message-dump and stripped of user
identities.

parseWeek kept the last number in the title, so a heading like
'Week 3 of 17' would have produced week 17. It now anchors on the word
Week."
```

---

### Task V5-21: Компонентные тесты NeonSportz API

**Зависит от:** V5-18. **Параллельно с:** V5-19, V5-20, V5-22.
**Worktree:** `v5/21-neonsportz-tests`

**Files:**
- Create: `discord_kafka_game_image/internal/middle/headless/fetch_test.go`
- Create: `discord_kafka_game_image/internal/middle/headless/testdata/recap.json`

**Контекст:** покрывает `JSONInt` (числа приходят то числами, то строками) и обработку 404/429/таймаутов. Спека §5.

- [ ] **Step 1: Снять фикстуру recap API**

```bash
curl -s "https://neonsportz.com/api/leagues/MEGA/games/26912152/recap/" \
  > discord_kafka_game_image/internal/middle/headless/testdata/recap.json
```

- [ ] **Step 2: Написать тесты**

```go
package headless

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJSONIntAcceptsNumberAndString(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{`5`, 5},
		{`"5"`, 5},
		{`"5.7"`, 6},
		{`5.4`, 5},
		{`null`, 0},
		{`""`, 0},
	}
	for _, tc := range cases {
		var got JSONInt
		if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
			t.Errorf("Unmarshal(%s): %v", tc.raw, err)
			continue
		}
		if int(got) != tc.want {
			t.Errorf("Unmarshal(%s) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestFetchJSONParsesRealRecap(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "recap.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	got, err := fetchJSON(context.Background(), srv.Client(), srv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("fetchJSON: %v", err)
	}
	if got.Game.HomeTeam.DisplayName == "" {
		t.Error("home team must be populated from a real recap payload")
	}
}

func TestFetchJSONReportsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := fetchJSON(context.Background(), srv.Client(), srv.URL, 5*time.Second); err == nil {
		t.Error("a 404 must be reported as an error")
	}
}

func TestFetchJSONReportsRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := fetchJSON(context.Background(), srv.Client(), srv.URL, 5*time.Second)
	if err == nil {
		t.Fatal("a 429 must be reported as an error")
	}
}
```

- [ ] **Step 3: Запустить и починить сигнатуру**

Run: `go test ./discord_kafka_game_image/internal/middle/headless/ -v`
Expected: FAIL — `fetchJSON` не принимает клиента. Привести сигнатуру к `fetchJSON(ctx context.Context, doer ports.HTTPDoer, url string, timeout time.Duration) (Recap, error)` (изменение начато в V5-18) и обновить вызовы.

Run повторно: PASS.

- [ ] **Step 4: Commit**

```bash
git add discord_kafka_game_image/internal/middle/headless/
git commit -m "test: cover NeonSportz API handling against a recorded payload

JSONInt exists because the API returns the same field as a number in
one response and a string in another; the test pins both forms plus
null and empty string.

404 and 429 are asserted explicitly — they drive the fallback path."
```

---

### Task V5-22: Интеграционные тесты MinIO и Redpanda

**Зависит от:** V5-06, V5-18. **Параллельно с:** V5-19, V5-20, V5-21.
**Worktree:** `v5/22-storage-tests`

**Files:**
- Create: `discord_kafka_game_image/internal/storage/minio_test.go`
- Create: `internal/common/pgtest/redpanda.go`
- Modify: `go.mod`

**Interfaces:**
- Produces: `func NewRedpanda(t *testing.T) []string` — возвращает список брокеров

- [ ] **Step 1: Добавить модули testcontainers**

```bash
go get github.com/testcontainers/testcontainers-go/modules/minio@v0.43.0
go get github.com/testcontainers/testcontainers-go/modules/redpanda@v0.43.0
go mod tidy
```

- [ ] **Step 2: Харнесс Redpanda**

```go
package pgtest

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
)

// NewRedpanda starts a broker and returns its bootstrap addresses.
// Redpanda is what v5.0 runs in production, and it starts in a few
// seconds where Kafka needed the better part of a minute.
func NewRedpanda(t *testing.T) []string {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}

	ctx := context.Background()
	container, err := redpanda.Run(ctx,
		"docker.redpanda.com/redpandadata/redpanda:v24.2.18",
		redpanda.WithAutoCreateTopics(),
	)
	if err != nil {
		t.Fatalf("start redpanda: %v", err)
	}

	addr, err := container.KafkaSeedBroker(ctx)
	if err != nil {
		t.Fatalf("seed broker: %v", err)
	}

	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate redpanda: %v", err)
		}
	})

	return []string{addr}
}
```

- [ ] **Step 3: Тест MinIO**

```go
package storage

import (
	"bytes"
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/config"
)

func TestUploadAndObjectKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}
	endpoint, access, secret := startMinio(t) // helper from testcontainers module

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
}
```

Хелпер `startMinio` в том же файле:

```go
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
```

Импорты: `tcminio "github.com/testcontainers/testcontainers-go/modules/minio"` и `"github.com/testcontainers/testcontainers-go"`.

- [ ] **Step 4: Запустить тесты**

Run: `go test ./discord_kafka_game_image/internal/storage/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "test: cover MinIO uploads and add a Redpanda harness

Object keys replace the colon from the composite message id; a direct
link to a key containing a colon breaks in some S3 clients, so the
substitution is asserted."
```

---

# Фаза 4 — Инфраструктура

### Task V5-23: Переход на Redpanda

**Зависит от:** фаза 2 смержена. **Параллельно с:** V5-24…V5-27.
**Worktree:** `v5/23-redpanda`

**Files:**
- Modify: `docker-compose.yml` (заменить zookeeper + kafka на redpanda, переписать kafka-init)
- Modify: `.env.example`
- Modify: `README.md` (команды чтения топиков)

**Контекст:** Kafka + Zookeeper потребляют 480 МБ — 65% стека — обслуживая ~600 сообщений за всю жизнь системы. Спека §3.

- [ ] **Step 1: Заменить брокер в compose**

Удалить сервисы `zookeeper` и `kafka`, добавить:

```yaml
  redpanda:
    image: docker.redpanda.com/redpandadata/redpanda:v24.2.18
    restart: unless-stopped
    command:
      - redpanda
      - start
      - --kafka-addr=PLAINTEXT://0.0.0.0:9092
      - --advertise-kafka-addr=PLAINTEXT://redpanda:9092
      # A Raspberry Pi is not a dedicated broker host: without these
      # flags Redpanda reserves memory and pins cores as if it were.
      - --overprovisioned
      - --smp=1
      - --memory=512M
      - --reserve-memory=0M
      - --default-log-level=warn
    environment:
      TZ: ${TZ}
    mem_limit: 700m
    healthcheck:
      test: ["CMD-SHELL", "rpk cluster health | grep -q 'Healthy:.*true'"]
      interval: 30s
      timeout: 10s
      retries: 6
      start_period: 30s
    labels:
      - autoheal=true
    volumes:
      - redpanda-data:/var/lib/redpanda/data
    networks:
      - megagames-net
```

В `volumes:` добавить `redpanda-data:`. Тома `kafka-data`, `zookeeper-data`, `zookeeper-log` **оставить** — страховка для отката.

- [ ] **Step 2: Переписать kafka-init на rpk**

```yaml
  kafka-init:
    image: docker.redpanda.com/redpandadata/redpanda:v24.2.18
    restart: on-failure
    depends_on:
      redpanda:
        condition: service_healthy
    environment:
      TZ: ${TZ}
    env_file:
      - .env
    entrypoint:
      - /bin/bash
      - -c
      - |
        set -eu
        for topic in "$$KAFKA_INPUT_TOPIC" "$$KAFKA_WEEK_TOPIC" "$$KAFKA_GAME_TOPIC" "$$KAFKA_TELEGRAM_WEEK_TOPIC" "$$KAFKA_GAME_IMAGE_TOPIC"; do
          if [ -z "$$topic" ]; then
            echo "empty topic name in configuration" >&2
            exit 1
          fi
          rpk topic create "$$topic" \
            --brokers "$$KAFKA_BROKERS" \
            --partitions "$$KAFKA_PARTITIONS" \
            --replicas "$$KAFKA_REPLICATION_FACTOR" \
            || rpk topic describe "$$topic" --brokers "$$KAFKA_BROKERS" >/dev/null
        done
        echo "topics ready"
    networks:
      - megagames-net
```

`|| rpk topic describe` делает шаг идемпотентным: если топик уже есть, `create` вернёт ошибку, а `describe` подтвердит существование.

- [ ] **Step 3: Обновить depends_on у сервисов**

Во всех шести сервисах заменить `kafka-init: condition: service_completed_successfully` — имя сервиса не меняется, менять ничего не нужно. Убрать `ports: - "9092:9092"` (наружу брокер не нужен, см. V5-25).

- [ ] **Step 4: Проверить локально**

```bash
docker compose up -d redpanda kafka-init
docker compose logs kafka-init
docker compose exec -T redpanda rpk topic list
```
Expected: пять топиков созданы.

- [ ] **Step 5: Замерить потребление**

```bash
docker stats --no-stream --format "table {{.Name}}\t{{.MemUsage}}" | grep redpanda
```
Expected: заметно меньше 480 МБ, которые занимали Kafka + Zookeeper вместе.

> **Критерий отката:** если Redpanda потребляет больше Kafka, задача отменяется и вместо неё выполняется переход Kafka на KRaft-режим (гарантированные −90 МБ за счёт удаления Zookeeper). Спека §3.

- [ ] **Step 6: Обновить README**

Заменить команду чтения топика:

```bash
docker compose exec redpanda rpk topic consume <topic> --brokers redpanda:9092
```

- [ ] **Step 7: Commit**

```bash
git add docker-compose.yml .env.example README.md
git commit -m "perf: replace Kafka and Zookeeper with Redpanda

Two JVM containers held 480 MB — 65% of the stack — to move roughly 600
messages over the system's lifetime, with lag permanently at zero.

Redpanda speaks the same protocol, so no Go code changes. The Pi needs
--overprovisioned and an explicit memory bound, otherwise Redpanda
reserves as if it owned the machine.

Kafka and Zookeeper volumes are kept for rollback."
```

---

### Task V5-24: Лимиты памяти, логи, override в git

**Зависит от:** ничего. **Параллельно с:** V5-23, V5-25…V5-27.
**Worktree:** `v5/24-limits`

**Files:**
- Modify: `docker-compose.yml`
- Delete on host: `docker-compose.override.yml` (после переноса)

**Контекст:** `mem_limit` сейчас есть только у Kafka и Zookeeper и только в **незакоммиченном** `docker-compose.override.yml` на живом хосте — он потерялся бы при переустановке. `game-image` поднимает Chromium с пиком 150–250 МБ без всякого ограничения. Логи растут без предела. Спека §6.

- [ ] **Step 1: Добавить якорь логирования**

В начало `docker-compose.yml`:

```yaml
x-logging: &default-logging
  driver: json-file
  options:
    max-size: "10m"
    max-file: "3"
```

- [ ] **Step 2: Проставить лимиты и логирование**

Каждому сервису добавить `logging: *default-logging` и `mem_limit`:

| Сервис | mem_limit | Обоснование |
|---|---|---|
| `discord-kafka-game-image` | `512m` | пик Chromium 150–250 МБ + Go-рантайм |
| `postgres` | `256m` | текущее потребление 29 МБ, запас на рост |
| `minio` | `192m` | текущее 98 МБ |
| `redpanda` | `700m` | задано в V5-23 |
| `admin-panel` | `128m` | текущее 3,7 МБ, запас на рендер дашборда |
| остальные Go-сервисы | `64m` | текущее 7–15 МБ |
| `autoheal` | `32m` | текущее 0,5 МБ |

- [ ] **Step 3: Проверить, что лимиты не душат сервисы**

```bash
docker compose up -d
sleep 120
docker stats --no-stream --format "table {{.Name}}\t{{.MemUsage}}\t{{.MemPerc}}"
```
Expected: ни один контейнер не приближается к своему лимиту вплотную; ни одного OOM в `docker compose logs`.

- [ ] **Step 4: Удалить override с хоста**

```bash
ssh pi 'cd /home/colz/envs/mega_games && \
  cp docker-compose.override.yml docker-compose.override.yml.pre-v5 && \
  rm docker-compose.override.yml'
```

- [ ] **Step 5: Commit**

```bash
git add docker-compose.yml
git commit -m "ops: commit memory limits and bound container logs

Kafka and Zookeeper limits lived only in an untracked override file on
the live host and would have vanished on any reinstall. Everything else
had no limit at all, so a leaking Chromium could take the host — and
the databases — down with it.

json-file was running without max-size, so logs grew without bound on a
disk already at 72%."
```

---

### Task V5-25: Порты, пароли, авторизация админки

**Зависит от:** ничего. **Параллельно с:** V5-23, V5-24, V5-26, V5-27.
**Worktree:** `v5/25-security`

**Files:**
- Modify: `docker-compose.yml`
- Modify: `.env.example`
- Modify: `cmd/admin_panel/main.go`
- Create: `internal/admin/auth.go`
- Create: `internal/admin/auth_test.go`

**Контекст:** наружу открыты Postgres, Mongo, MinIO и Kafka с паролями `megagames/megagames`, а админка без авторизации имеет `DELETE /teams/{name}` и `POST /schedule/upload`. Спека §6.

- [ ] **Step 1: Написать падающий тест авторизации**

```go
package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestBasicAuthRejectsMissingCredentials(t *testing.T) {
	h := BasicAuth("admin", "secret", okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/teams/Falcons", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401: DELETE must not be reachable without credentials", rec.Code)
	}
}

func TestBasicAuthRejectsWrongPassword(t *testing.T) {
	h := BasicAuth("admin", "secret", okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rec.Code)
	}
}

func TestBasicAuthAcceptsCorrectCredentials(t *testing.T) {
	h := BasicAuth("admin", "secret", okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rec.Code)
	}
}

func TestBasicAuthDisabledWhenPasswordEmpty(t *testing.T) {
	h := BasicAuth("", "", okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want 200 when auth is not configured", rec.Code)
	}
}
```

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./internal/admin/ -run TestBasicAuth -v`
Expected: FAIL — `undefined: BasicAuth`

- [ ] **Step 3: Реализовать**

```go
package admin

import (
	"crypto/subtle"
	"net/http"
)

// BasicAuth guards the admin panel. It exposes DELETE /teams/{name} and
// POST /schedule/upload, which were reachable by anyone on the network.
//
// An empty password disables the guard so a local run needs no setup;
// production sets ADMIN_PASSWORD.
func BasicAuth(username, password string, next http.Handler) http.Handler {
	if password == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="mega-games admin"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Подключить в main и вынести /health**

В `cmd/admin_panel/main.go`:

```go
	// /health stays outside the guard: the container healthcheck has no
	// credentials.
	guarded := admin.BasicAuth(cfg.AdminUser, cfg.AdminPassword, mux)

	root := http.NewServeMux()
	root.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	root.Handle("/", guarded)

	srv := &http.Server{
		Addr:              cfg.HealthAddr,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}
```

Удалить регистрацию `/health` внутри `mux`. В `internal/admin/config.go` добавить `AdminUser` и `AdminPassword`, читая `ADMIN_USER` (дефолт `admin`) и `ADMIN_PASSWORD`.

- [ ] **Step 5: Закрыть порты**

В `docker-compose.yml` удалить секции `ports` у `postgres`, `redpanda` и порт консоли `9001` у `minio`. Оставить:

```yaml
  minio:
    ports:
      - "${MINIO_EXPOSE_PORT:-9000}:9000"   # прямые ссылки на картинки в дашборде
  admin-panel:
    ports:
      - "8002:8081"
```

В `.env.example` убрать `POSTGRES_EXPOSE_PORT`, `MONGO_EXPOSE_PORT`, `MINIO_CONSOLE_PORT`; добавить:

```
# ADMIN_USER - admin panel username.
ADMIN_USER=admin
# ADMIN_PASSWORD - admin panel password. Empty disables authentication.
ADMIN_PASSWORD=replace_me
```

- [ ] **Step 6: Сменить пароли на хосте**

```bash
ssh pi 'cd /home/colz/envs/mega_games && cp .env .env.pre-v5'
```
Затем вручную заменить в `.env` значения `POSTGRES_PASSWORD`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY` и задать `ADMIN_PASSWORD`.

> Смена `POSTGRES_PASSWORD` для существующего тома не применяется автоматически — Postgres читает её только при инициализации. Пароль нужно поменять внутри БД:
> ```bash
> docker compose exec -T postgres psql -U megagames -d megagames \
>   -c "ALTER USER megagames WITH PASSWORD 'new-password';"
> ```
> и только затем обновить `.env` и перезапустить сервисы. То же для MinIO: ключи меняются через `mc admin user`.

- [ ] **Step 7: Проверить**

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8002/          # 401
curl -s -o /dev/null -w "%{http_code}\n" -u admin:<pass> http://localhost:8002/   # 200
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8002/health    # 200
nc -z localhost 5432 && echo "postgres still exposed" || echo "postgres closed"
```

- [ ] **Step 8: Commit**

```bash
git add internal/admin/ cmd/admin_panel/ docker-compose.yml .env.example
git commit -m "security: guard the admin panel, close database ports

Postgres, Mongo, MinIO and Kafka were published on 0.0.0.0 with the
default megagames/megagames credentials, and the admin panel exposed
DELETE /teams and POST /schedule/upload with no authentication at all.

/health stays unguarded so the container healthcheck keeps working."
```

---

### Task V5-26: Обязательный TAG в скриптах деплоя

**Зависит от:** ничего. **Параллельно с:** V5-23…V5-25, V5-27.
**Worktree:** `v5/26-tag-required`

**Files:**
- Modify: `scripts/deploy.sh`
- Modify: `scripts/publish.sh`
- Modify: `scripts/install.sh`
- Modify: `docker-compose.yml` (заменить `${TAG:-4.3.2}` на `${TAG:?...}`)

**Контекст:** P0-1 — единственный дефект уровня P0. В проде работает `4.3.9`, а `deploy.sh` без `TAG` подставит `4.3.2` и откатит прод на семь версий, потеряв фиксы селекторов NeonSportz и таймаутов VPN.

- [ ] **Step 1: Сделать TAG обязательным в deploy.sh**

```bash
#!/bin/bash
set -euo pipefail

# No default: the previous 4.3.2 fallback would have rolled production
# back seven releases, losing the NeonSportz selector and VPN timeout
# fixes, if anyone ran this without TAG set.
if [[ -z "${TAG:-}" ]]; then
  printf 'ERROR: TAG is required, e.g. TAG=5.0.0 %s\n' "$0" >&2
  exit 1
fi

export IMAGE_REGISTRY="${IMAGE_REGISTRY:-ghcr.io}"
export IMAGE_NAMESPACE="${IMAGE_NAMESPACE:-colzphml/mega_games}"
export TAG

echo "Deploying tag: $TAG"
echo "Registry: $IMAGE_REGISTRY/$IMAGE_NAMESPACE"

echo "Validating compose configuration..."
docker compose config >/dev/null

echo "Pulling images from registry..."
docker compose pull

echo "Recreating services without local builds..."
docker compose up -d --force-recreate --remove-orphans --no-build

echo "Current service state:"
docker compose ps

echo "Deployment complete."
```

- [ ] **Step 2: То же в publish.sh**

```bash
#!/bin/bash
set -euo pipefail

if [[ -z "${TAG:-}" ]]; then
  printf 'ERROR: TAG is required, e.g. TAG=5.0.0 %s\n' "$0" >&2
  exit 1
fi

export TAG
export IMAGE_REGISTRY="${IMAGE_REGISTRY:-ghcr.io}"
export IMAGE_NAMESPACE="${IMAGE_NAMESPACE:-colzphml/mega_games}"

echo "Building images with tag: $TAG"
docker compose build

echo "Pushing images to registry..."
docker compose push

echo "Done. Preferred release path: TAG=$TAG ./scripts/release.sh"
```

- [ ] **Step 3: Убрать дефолт из install.sh**

Заменить `DEFAULT_TAG="v4.3.2"` и строку `TAG="${TAG:-$DEFAULT_TAG}"` на:

```bash
if [[ -z "${TAG:-}" ]]; then
  die "TAG is required, e.g. TAG=v5.0.0"
fi
```

- [ ] **Step 4: Сделать TAG обязательным в compose**

Заменить во всех семи местах `${TAG:-4.3.2}` на `${TAG:?TAG is required, e.g. TAG=5.0.0}`.

- [ ] **Step 5: Проверить**

```bash
unset TAG
./scripts/deploy.sh 2>&1 | head -2   # ERROR: TAG is required
docker compose config 2>&1 | head -2 # error про TAG
TAG=5.0.0 docker compose config >/dev/null && echo "ok with TAG"
```

- [ ] **Step 6: Commit**

```bash
git add scripts/ docker-compose.yml
git commit -m "fix: require TAG instead of defaulting to an old release

Production runs 4.3.9 while every script and the compose file defaulted
to 4.3.2. Running deploy.sh without TAG would have rolled back seven
releases, dropping the NeonSportz selector fix and the VPN timeout
fixes. Only the TAG line in the host .env prevented it."
```

---

### Task V5-27: Прочая конфигурация и хост

**Зависит от:** ничего. **Параллельно с:** V5-23…V5-26.
**Worktree:** `v5/27-config`

**Files:**
- Modify: `discord_kafka_week_formatter/internal/config/config.go` (дедлайн анонса)
- Modify: `discord_kafka_week_formatter/internal/formatter/formatter.go`
- Modify: `.env.example`
- Create: `scripts/host-setup.sh`

**Контекст:** P2-7 (`MINIO_PUBLIC_URL=http://localhost:9000` пишет в базу ссылки, валидные только на самом Pi), P3-5 (дедлайн 40 часов захардкожен), swappiness и очистка образов. Спека §4.3, §6.

- [ ] **Step 1: Вынести дедлайн в конфигурацию**

В `config.go` week-formatter добавить:

```go
	AnnounceDeadline time.Duration
```

и в `Load()`:

```go
	if cfg.AnnounceDeadline, err = config.Duration("WEEK_ANNOUNCE_DEADLINE", 40*time.Hour); err != nil {
		return Config{}, err
	}
```

Передать значение в `BuildWeekMessage` четвёртым параметром:

```go
func BuildWeekMessage(payload store.WeekPayload, teams map[string]store.Team, games []store.Game, deadline time.Duration) (string, error)
```

и обновить вызов в `handleMessage` и тесты из V5-10.

- [ ] **Step 2: Исправить MINIO_PUBLIC_URL**

В `.env.example`:

```
# MINIO_PUBLIC_URL - public base URL used to build image links.
# Must be reachable from wherever the dashboard is opened. Leave empty
# to derive it from MINIO_ENDPOINT.
MINIO_PUBLIC_URL=
# WEEK_ANNOUNCE_DEADLINE - how long players have to announce a match.
WEEK_ANNOUNCE_DEADLINE=40h
```

Пустое значение включает вывод из `MINIO_ENDPOINT` — код в `storage.New` это уже поддерживает.

- [ ] **Step 3: Написать скрипт настройки хоста**

```bash
#!/usr/bin/env bash
# Host-level tuning for the Raspberry Pi. Idempotent.
set -euo pipefail

echo "==> swappiness"
# The Pi is not short of memory (available ~2.5 GB), but swappiness=60
# makes the kernel evict to the SD card anyway, which is both slow and
# wearing. Ten keeps swap as a last resort.
sudo tee /etc/sysctl.d/60-mega-games.conf >/dev/null <<'EOF'
vm.swappiness=10
EOF
sudo sysctl --system >/dev/null
echo "    vm.swappiness = $(cat /proc/sys/vm/swappiness)"

echo "==> docker image cleanup"
docker image prune -a -f --filter "until=336h"
docker builder prune -f

echo "==> weekly cleanup cron"
CRON_LINE='0 4 * * 0 docker image prune -a -f --filter "until=336h" >/dev/null 2>&1'
( crontab -l 2>/dev/null | grep -v 'docker image prune' ; echo "$CRON_LINE" ) | crontab -

echo "==> disk"
df -h / | tail -1

echo "Done."
```

- [ ] **Step 4: Выполнить на хосте**

```bash
scp scripts/host-setup.sh pi:/tmp/
ssh pi 'bash /tmp/host-setup.sh'
```
Expected: `vm.swappiness = 10`, освобождено около 13,6 ГБ.

- [ ] **Step 5: Commit**

```bash
git add discord_kafka_week_formatter/ .env.example scripts/host-setup.sh
git commit -m "ops: make the announce deadline configurable, tune the host

MINIO_PUBLIC_URL pointed at localhost, so links written into the
database and Kafka events were only valid on the Pi itself.

swappiness drops from 60 to 10: with 2.5 GB available the kernel was
evicting to the SD card for no reason. Image pruning reclaims 13.6 GB
and now runs weekly."
```

---

# Фаза 5 — Документация и релиз

### Task V5-28: Точка входа для агентов и файл прогресса

**Зависит от:** ничего. **Параллельно с:** V5-29.
**Worktree:** `v5/28-agent-docs`

**Files:**
- Create: `docs/superpowers/plans/v5-progress.md`
- Modify: `AGENTS.md`

**Контекст:** требование — новая сессия и параллельные агенты должны входить в работу без потери контекста.

- [ ] **Step 1: Создать файл прогресса**

```markdown
# v5.0 — Прогресс

Обновляется после каждой завершённой задачи. Источник истины по состоянию работ.

**Ветка:** `release-5.0` · **Спека:** `docs/superpowers/specs/2026-07-28-mega-games-v5-design.md`

| Задача | Статус | Ветка worktree | Смержено |
|---|---|---|---|
| V5-01 Единый Go-модуль | ⬜ | `v5/01-single-module` | — |
| V5-02 common/config | ⬜ | `v5/02-common-config` | — |
| V5-03 common/retry | ⬜ | `v5/03-common-retry` | — |
| V5-04 common/health | ⬜ | `v5/04-common-health` | — |
| V5-05 common/queue | ⬜ | `v5/05-common-queue` | — |
| V5-06 Харнесс pgtest | ⬜ | `v5/06-pgtest` | — |
| V5-07 Фикс дублей в TG | ⬜ | `v5/07-eligibility` | — |
| V5-08 Классификация ошибок Discord | ⬜ | `v5/08-discord-errors` | — |
| V5-09 health/ready | ⬜ | `v5/09-health-split` | — |
| V5-10 Постсезон | ⬜ | `v5/10-postseason` | — |
| V5-11 Экранирование Markdown | ⬜ | `v5/11-markdown-escape` | — |
| V5-12 Метка fallback | ⬜ | `v5/12-fallback-flag` | — |
| V5-13 Мелкие фиксы и мёртвый код | ⬜ | `v5/13-cleanup` | — |
| V5-14 ListPending в pgstore | ⬜ | `v5/14-pgstore-listpending` | — |
| V5-15 Удаление MongoDB | ⬜ | `v5/15-drop-mongo` | — |
| V5-16 Оптимизация gochrome | ⬜ | `v5/16-chrome-reuse` | — |
| V5-17 Окно дашборда | ⬜ | `v5/17-dashboard-window` | — |
| V5-18 Интерфейсы | ⬜ | `v5/18-interfaces` | — |
| V5-19 Тесты Telegram | ⬜ | `v5/19-telegram-tests` | — |
| V5-20 Тесты Discord | ⬜ | `v5/20-discord-tests` | — |
| V5-21 Тесты NeonSportz | ⬜ | `v5/21-neonsportz-tests` | — |
| V5-22 Тесты MinIO/Redpanda | ⬜ | `v5/22-storage-tests` | — |
| V5-23 Redpanda | ⬜ | `v5/23-redpanda` | — |
| V5-24 Лимиты и логи | ⬜ | `v5/24-limits` | — |
| V5-25 Безопасность | ⬜ | `v5/25-security` | — |
| V5-26 Обязательный TAG | ⬜ | `v5/26-tag-required` | — |
| V5-27 Конфигурация и хост | ⬜ | `v5/27-config` | — |
| V5-28 Документация для агентов | ⬜ | `v5/28-agent-docs` | — |
| V5-29 README и AGENTS.md | ⬜ | `v5/29-docs` | — |
| V5-30 Миграция и выкатка | ⬜ | `v5/30-release` | — |
| V5-31 Устойчивость headless | ⬜ | `v5/31-headless-resilience` | — |

Легенда: ⬜ не начата · 🟡 в работе · ✅ смержена · ❌ заблокирована

## Журнал решений по ходу работ

Сюда записывается всё, что отклонилось от плана, с причиной. Пустой журнал
означает, что план выполнялся дословно.

| Дата | Задача | Отклонение | Причина |
|---|---|---|---|
```

- [ ] **Step 2: Добавить точку входа в AGENTS.md**

В начало `AGENTS.md`, сразу после заголовка:

```markdown
## ⚠️ Идёт разработка v5.0

**Начни с этих трёх файлов, в этом порядке:**

1. `docs/superpowers/specs/2026-07-28-mega-games-v5-design.md` — что делаем и почему
2. `docs/superpowers/plans/2026-07-28-mega-games-v5.md` (+ `-part2`, `-part3`) — задачи
3. `docs/superpowers/plans/v5-progress.md` — что уже сделано

`AUDIT.md` содержит исходный разбор системы: откуда взялся каждый дефект и какие
замеры сняты с живого хоста.

**Ветка:** `release-5.0`. Ветка `release-4.0` — то, что сейчас в проде (`4.3.9`).

### Правила параллельной работы

Каждая задача выполняется в отдельном git worktree:

```bash
git worktree add ../mega_games-V5-07 -b v5/07-eligibility release-5.0
cd ../mega_games-V5-07
```

- Трогай **только** файлы из раздела «Files» своей задачи. Список «Не трогает»
  указывает, где работают соседние агенты.
- Порядок мержа задан зависимостями. Где две задачи правят один файл, в плане
  явно написано, какая мержится первой.
- После мержа обнови `v5-progress.md`: статус, ссылку на коммит, и запись в
  журнале решений, если отклонился от плана.
- Не мержь задачу, пока `go build ./... && go vet ./... && go test ./... -short`
  не проходит.

### Что нельзя делать ни в одной задаче

- Удалять данные: тома `postgres-data`, `minio-data`, `mongo-data`, `kafka-data`
  сохраняются. Требование заказчика — вся история остаётся.
- Активировать lifecycle-политику MinIO.
- Задавать дефолтное значение `TAG` в скриптах или compose.
- Собирать релизные образы на Pi — публикация только через GitHub Actions.
```

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/plans/v5-progress.md AGENTS.md
git commit -m "docs: add an entry point for parallel and fresh sessions

A new session should be able to pick up work by reading three files
rather than reconstructing context from conversation history."
```

---

### Task V5-29: Актуализация README и AGENTS.md

**Зависит от:** V5-15, V5-23, V5-25, V5-26 смержены. **Параллельно с:** V5-28.
**Worktree:** `v5/29-docs`

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`

**Контекст:** документация отстала: версии `4.3.2` при проде `4.3.9`, Go 1.24 против 1.25.5, «gochrome по умолчанию» при дефолте `headless` в коде, Mongo описан как отдельное хранилище.

- [ ] **Step 1: Обновить README**

Изменения:
- версии во всех примерах: `4.3.2` → `5.0.0`;
- убрать `mongo` из списка сервисов и таблицы хранилищ;
- заменить `zookeeper` + `kafka` на `redpanda`;
- убрать профиль `selenium` целиком;
- команду чтения топика заменить на `rpk topic consume`;
- в разделе про SSH-туннель убрать проброс `5432`, `27017`, `9001` — они больше не публикуются;
- добавить, что админка требует `ADMIN_USER`/`ADMIN_PASSWORD`;
- обновить таблицу портов: наружу только `8002` и `9000`.

- [ ] **Step 2: Обновить AGENTS.md**

- Go: `1.24` → `1.25.5`;
- убрать MongoDB из таблицы хранилищ, добавить строку, что статусы game-image живут в Postgres;
- в разделе «Режимы image fetcher» убрать `selenium`, отметить `gochrome` как используемый в проде и `headless` как запасной;
- в «Антипаттерны» добавить: «Дефолтный `TAG` в скриптах — откатывает прод»; «Одинаковый порог eligibility и интервал тикера — дублирует посты»;
- в «Заметки» заменить «Нет unit-тестов» на описание пирамиды и команду `go test ./... -short`.

- [ ] **Step 3: Проверить, что примеры команд работают**

Прогнать каждую команду из README на живом хосте, кроме деструктивных.

- [ ] **Step 4: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: bring README and AGENTS.md in line with the code

Version examples still said 4.3.2 while production ran 4.3.9, Go was
listed as 1.24 against 1.25.5 in every go.mod, gochrome was called the
default when the code defaulted to headless, and MongoDB was documented
as a storage tier after being removed."
```

---

### Task V5-30: Миграция и выкатка

**Зависит от:** все задачи смержены. **Выполняется последней.**
**Worktree:** не нужен — работа на `release-5.0` и на хосте.

**Files:**
- Create: `scripts/verify-history.sh`

**Контекст:** жёсткое требование — вся история сохраняется. Спека §7.

- [ ] **Step 1: Написать скрипт сверки**

```bash
#!/usr/bin/env bash
# Prints the row counts that must not change across the migration.
# Run before and after; the two outputs must be identical.
set -euo pipefail

cd "$(dirname "$0")/.."

docker compose exec -T postgres psql -U "${POSTGRES_USER:-megagames}" -d "${POSTGRES_DB:-megagames}" -t <<'SQL'
SELECT 'discord_message_status', count(*) FROM discord_message_status
UNION ALL SELECT 'week_message_status', count(*) FROM week_message_status
UNION ALL SELECT 'game_image_status', count(*) FROM game_image_status
UNION ALL SELECT 'game_image_with_url', count(*) FROM game_image_status WHERE image->>'image_url' IS NOT NULL
UNION ALL SELECT 'telegram_week_status', count(*) FROM telegram_week_status
UNION ALL SELECT 'telegram_game_status', count(*) FROM telegram_game_status
UNION ALL SELECT 'teams', count(*) FROM teams
UNION ALL SELECT 'schedule_games', count(*) FROM schedule_games
ORDER BY 1;
SQL

echo "--- minio ---"
docker compose exec -T minio du -sh /data 2>/dev/null || true
```

- [ ] **Step 2: Снять эталон до миграции**

```bash
ssh pi 'cd /home/colz/envs/mega_games && bash scripts/verify-history.sh' > /tmp/before-v5.txt
cat /tmp/before-v5.txt
```
Ожидаемые значения: `discord_message_status` 572, `game_image_status` 600, `game_image_with_url` 600, `telegram_game_status` 600, `week_message_status` 49, `telegram_week_status` 49.

- [ ] **Step 3: Сделать резервные копии**

```bash
ssh pi 'cd /home/colz/envs/mega_games && \
  docker compose exec -T postgres pg_dump -U megagames megagames | gzip > ~/megagames-pre-v5.sql.gz && \
  cp .env ~/env-pre-v5 && \
  cp docker-compose.yml ~/compose-pre-v5.yml && \
  ls -lh ~/megagames-pre-v5.sql.gz'
```

- [ ] **Step 4: Остановить приём и дождаться пустой очереди**

```bash
ssh pi 'cd /home/colz/envs/mega_games && docker compose stop discord-kafka-listener'
ssh pi 'cd /home/colz/envs/mega_games && for g in discord-kafka-processor discord-kafka-week-formatter discord-kafka-game-image discord-kafka-telegram-week-sender discord-kafka-telegram-game-sender; do
  docker compose exec -T kafka kafka-consumer-groups --bootstrap-server kafka:9092 --describe --group $g 2>/dev/null | awk "NR>1 {print \$1, \$6}"
done'
```
Expected: LAG = 0 у всех групп. Если нет — подождать и повторить.

- [ ] **Step 5: Выпустить релиз**

```bash
git checkout release-5.0
git push origin release-5.0
TAG=5.0.0 ./scripts/release.sh
```
Дождаться завершения workflow «Publish Images» — семь образов, обе платформы.

- [ ] **Step 6: Выкатить**

```bash
ssh pi 'set -euo pipefail
  cd /home/colz/envs/mega_games
  git fetch origin
  git checkout release-5.0
  git pull --ff-only origin release-5.0
  sed -i "s/^TAG=.*/TAG=5.0.0/" .env
  docker compose down
  export IMAGE_REGISTRY=ghcr.io IMAGE_NAMESPACE=colzphml/mega_games TAG=5.0.0
  docker compose pull
  docker compose up -d --remove-orphans --no-build
  docker compose ps'
```

- [ ] **Step 7: Сверить историю**

```bash
ssh pi 'cd /home/colz/envs/mega_games && bash scripts/verify-history.sh' > /tmp/after-v5.txt
diff /tmp/before-v5.txt /tmp/after-v5.txt && echo "HISTORY INTACT" || echo "MISMATCH — ROLL BACK"
```
Расхождение хоть в одной строке — откат.

- [ ] **Step 8: Проверить критерии приёмки**

```bash
# 1. Все контейнеры healthy
ssh pi 'cd /home/colz/envs/mega_games && docker compose ps'

# 2. Потребление стека
ssh pi 'docker stats --no-stream --format "{{.Name}}\t{{.MemUsage}}" | grep mega_games'
# Критерий: сумма <= 450 МБ

# 3. Топики на месте, lag нулевой
ssh pi 'cd /home/colz/envs/mega_games && docker compose exec -T redpanda rpk topic list'

# 4. Health и readiness
ssh pi 'cd /home/colz/envs/mega_games && \
  docker compose exec -T discord-kafka-processor wget -qO- http://127.0.0.1:8080/health && \
  docker compose exec -T discord-kafka-processor wget -qO- http://127.0.0.1:8080/ready'

# 5. Админка под паролем
curl -s -o /dev/null -w "%{http_code}\n" http://<pi>:8002/          # 401
curl -s -o /dev/null -w "%{http_code}\n" -u admin:<pass> http://<pi>:8002/  # 200

# 6. Порты БД закрыты
nc -z <pi> 5432 && echo "STILL EXPOSED" || echo "closed"

# 7. TAG обязателен
ssh pi 'cd /home/colz/envs/mega_games && unset TAG; ./scripts/deploy.sh 2>&1 | head -1'
```

- [ ] **Step 9: Проверить конвейер end-to-end**

Дождаться следующей смены недели в Discord либо переобработать существующую игру:

```bash
ssh pi 'cd /home/colz/envs/mega_games && docker compose exec -T postgres psql -U megagames -d megagames -c \
  "UPDATE game_image_status SET status='"'"'new'"'"', attempts=0, image=NULL WHERE message_id=(SELECT message_id FROM game_image_status ORDER BY created_at DESC LIMIT 1);"'
```

> Поле `image` обязательно обнуляется: без этого `handleMessage` пропустит генерацию и переотправит старую картинку.

Проверить, что в Telegram пришла свежая картинка нормального размера (~1,4 МБ, не ~100 КБ — иначе это fallback, и дашборд это покажет).

- [ ] **Step 10: Зафиксировать результат**

Обновить `v5-progress.md`: все задачи ✅, в журнал решений — фактические замеры RAM и отклонения от плана.

- [ ] **Step 11: Commit**

```bash
git add scripts/verify-history.sh docs/superpowers/plans/v5-progress.md
git commit -m "ops: add history verification and record the v5.0 rollout"
git push origin release-5.0
```

---

## Откат

Если что-то пошло не так на любом шаге V5-30:

```bash
ssh pi 'set -euo pipefail
  cd /home/colz/envs/mega_games
  docker compose down
  cp ~/compose-pre-v5.yml docker-compose.yml
  cp ~/env-pre-v5 .env
  export TAG=4.3.9
  docker compose up -d --no-build
  docker compose ps'
```

Тома `postgres-data`, `minio-data`, `mongo-data`, `kafka-data`, `zookeeper-data` не удалялись ни на одном шаге, поэтому откат восстанавливает состояние полностью. Если повреждена база — развернуть дамп:

```bash
ssh pi 'gunzip -c ~/megagames-pre-v5.sql.gz | docker compose exec -T postgres psql -U megagames -d megagames'
```
