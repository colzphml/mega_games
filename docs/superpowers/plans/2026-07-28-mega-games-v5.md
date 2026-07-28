# MEGA Games Bot v5.0 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Сократить потребление RAM стека с ~742 МБ до ~395 МБ, закрыть дефекты P0/P1/P2 из аудита и покрыть код тестами так, чтобы будущие изменения проверялись автоматически.

**Architecture:** Семь сервисов и поток данных остаются без изменений. Kafka + Zookeeper заменяются на Redpanda (Kafka API-совместим, код Go не меняется), MongoDB удаляется как полный дубль Postgres. Семь отдельных Go-модулей сливаются в один, что открывает путь к общему пакету `internal/common` и сквозным тестам.

**Tech Stack:** Go 1.25.5, Redpanda, PostgreSQL 16, MinIO, chromedp, testcontainers-go v0.43.0, Docker Compose.

## Global Constraints

- Ветка: `release-5.0`. Каждая задача выполняется в отдельном git worktree с веткой `v5/<task-id>`, мержится в `release-5.0` в порядке зависимостей.
- Go: `1.25.5` во всех `go.mod` (сейчас корневой — `1.24.0`, требуется выравнивание).
- Module path после слияния: `github.com/colzphml/mega_games`.
- **Вся история сохраняется.** Эталон: `discord_message_status` 572 processed, `game_image_status` 600 (все с `image_url`), `telegram_game_status` 600, `week_message_status` 49, `telegram_week_status` 49, MinIO `game-images` 818 МБ / 600 объектов. Ни одна задача не удаляет данные.
- Схема Postgres не меняется миграциями. Тома `postgres-data`, `minio-data`, `mongo-data` не удаляются.
- Lifecycle-политика MinIO в этом релизе **не активируется**.
- Дефолтные значения `TAG` в скриптах запрещены — переменная обязательна.
- После каждой задачи обновляется `docs/superpowers/plans/v5-progress.md`.
- Спека: `docs/superpowers/specs/2026-07-28-mega-games-v5-design.md`. Аудит: `AUDIT.md`.

## Порядок и параллелизм

```
Фаза 0 (последовательно):  V5-01 → V5-02 → V5-03 → V5-04 → V5-05
Фаза 1:                    V5-06
Фаза 2 (параллельно):      V5-07 V5-08 V5-09 V5-10 V5-11 V5-12 V5-13 V5-14 V5-16 V5-17 V5-31
                           V5-15 после V5-14 и V5-12
Фаза 3 (последовательно):  V5-18 → затем параллельно V5-19 V5-20 V5-21 V5-22
Фаза 4 (параллельно):      V5-23 V5-24 V5-25 V5-26 V5-27
Фаза 5:                    V5-28 V5-29 → V5-30 (последняя)
```

**Пересечения файлов внутри фазы 2** — задачи независимы, кроме трёх пар, где
порядок мержа задан явно:

```
processor.go (game_image):  V5-12 → V5-15
gochrome.go:                V5-12 → V5-16
headless.go:                V5-13 → V5-31
internal/admin/store.go:    V5-12 → V5-17
```

---

# Фаза 0 — Фундамент

### Task V5-01: Слияние в единый Go-модуль

**Зависит от:** ничего. **Блокирует:** всё.
**Worktree:** `v5/01-single-module`

**Files:**
- Modify: `go.mod` (добавить зависимости всех сервисов, `go 1.25.5`)
- Delete: `discord_kafka_listener/go.mod`, `discord_kafka_listener/go.sum`, и так же для `discord_kafka_processor`, `discord_kafka_week_formatter`, `discord_kafka_game_image`, `discord_kafka_telegram_week_sender`, `discord_kafka_telegram_game_sender`
- Create: `.dockerignore`
- Modify: `discord_kafka_listener/Dockerfile`, `discord_kafka_processor/Dockerfile`, `discord_kafka_week_formatter/Dockerfile`, `discord_kafka_game_image/Dockerfile`, `discord_kafka_telegram_week_sender/Dockerfile`, `discord_kafka_telegram_game_sender/Dockerfile`

**Interfaces:**
- Produces: единый module path `github.com/colzphml/mega_games`; все сервисы импортируются как `github.com/colzphml/mega_games/discord_kafka_<name>/internal/...`; путь `github.com/colzphml/mega_games/internal/common/...` доступен всем сервисам.

**Контекст:** сейчас семь модулей без `go.work`, Dockerfile копирует только свой подкаталог (`COPY discord_kafka_listener/ ./`). В такой структуре общий пакет не соберётся. Конфликт версий один — `zerolog` v1.32.0 против v1.34.0, берётся старшая.

- [ ] **Step 1: Собрать объединённый go.mod**

```bash
cd /path/to/worktree
# сохранить списки зависимостей
cat discord_kafka_*/go.mod > /tmp/all-deps.txt
```

Записать в корневой `go.mod`:

```
module github.com/colzphml/mega_games

go 1.25.5

require (
	github.com/bwmarrin/discordgo v0.29.0
	github.com/chromedp/cdproto v0.0.0-20250724212937-08a3db8b4327
	github.com/chromedp/chromedp v0.14.2
	github.com/fogleman/gg v1.3.0
	github.com/go-telegram-bot-api/telegram-bot-api v4.6.4+incompatible
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0
	github.com/jackc/pgx/v5 v5.8.0
	github.com/minio/minio-go/v7 v7.0.76
	github.com/rs/zerolog v1.34.0
	github.com/segmentio/kafka-go v0.4.50
	github.com/tebeka/selenium v0.9.9
	go.mongodb.org/mongo-driver v1.17.1
	golang.org/x/image v0.18.0
)
```

- [ ] **Step 2: Удалить модульные файлы сервисов**

```bash
rm discord_kafka_listener/go.mod discord_kafka_listener/go.sum
rm discord_kafka_processor/go.mod discord_kafka_processor/go.sum
rm discord_kafka_week_formatter/go.mod discord_kafka_week_formatter/go.sum
rm discord_kafka_game_image/go.mod discord_kafka_game_image/go.sum
rm discord_kafka_telegram_week_sender/go.mod discord_kafka_telegram_week_sender/go.sum
rm discord_kafka_telegram_game_sender/go.mod discord_kafka_telegram_game_sender/go.sum
go mod tidy
```

`discord_tools/go.mod` **не трогать** — это отдельная утилита, не часть сборки сервисов.

- [ ] **Step 3: Проверить, что всё компилируется**

Run: `go build ./...`
Expected: успешная сборка, без ошибок импорта.

Run: `go vet ./...`
Expected: пустой вывод.

- [ ] **Step 4: Создать .dockerignore**

```
.git
.github
docs
monitoring
screens
*.md
!README.md
MEGA_games.csv
MEGA_teams.csv
main
admin_panel
.env
.env.*
*.bak-*
discord_tools
```

- [ ] **Step 5: Переписать Dockerfile сервисов**

Для каждого из шести сервисов заменить блок сборки. Пример для `discord_kafka_listener/Dockerfile` (остальные — аналогично, меняется только имя сервиса в двух местах):

```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.25.5-alpine AS build

SHELL ["/bin/sh", "-c"]

ARG TARGETOS
ARG TARGETARCH
ARG VERSION
ARG COMMIT
ARG BUILD_DATE
ARG TZ=Europe/Moscow

ENV TZ=$TZ

WORKDIR /src

RUN apk add --no-cache tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN set -eu; \
    GOOS="${TARGETOS:-linux}"; \
    GOARCH="${TARGETARCH:-amd64}"; \
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -o /out/discord-kafka-listener \
    -ldflags "-s -w -X main.buildVersion=${VERSION:-dev} -X main.buildCommit=${COMMIT:-unknown} -X main.buildDate=${BUILD_DATE:-unknown}" \
    ./discord_kafka_listener/cmd/discord-kafka-listener

FROM alpine:3.21

ARG TZ=Europe/Moscow
ENV TZ=$TZ

RUN apk add --no-cache tzdata

RUN adduser -D -g '' app
USER app
WORKDIR /app

COPY --from=build /out/discord-kafka-listener /app/discord-kafka-listener

ENTRYPOINT ["/app/discord-kafka-listener"]
```

Ключевые отличия от текущего: `COPY .git ./.git` удалён (версия приходит через `build-args` из CI), контекст — весь репозиторий, путь к `cmd` теперь включает подкаталог сервиса. Для `discord_kafka_game_image/Dockerfile` сохранить строку `RUN apk add --no-cache tzdata chromium nss freetype harfbuzz ttf-freefont` в финальной стадии.

- [ ] **Step 6: Проверить сборку образа**

Run: `docker build -f discord_kafka_listener/Dockerfile -t test-listener .`
Expected: успешная сборка.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor: merge seven Go modules into a single module

Dockerfiles copied only their own subdirectory, so a shared package
could never compile. One module per repository makes internal/common
reachable from every service and lets go test ./... cover everything.

zerolog resolves to v1.34.0, the newer of the two versions in use."
```

---

### Task V5-02: Пакет internal/common/config

**Зависит от:** V5-01. **Блокирует:** V5-07, V5-09, V5-18.
**Worktree:** `v5/02-common-config`

**Files:**
- Create: `internal/common/config/env.go`
- Create: `internal/common/config/env_test.go`

**Interfaces:**
- Produces:
  - `func Required(key string) (string, error)`
  - `func Optional(key, def string) string`
  - `func Duration(key string, def time.Duration) (time.Duration, error)`
  - `func PositiveInt(key string, def int) (int, error)`
  - `func NonNegativeInt(key string, def int) (int, error)`
  - `func Bool(key string, def bool) (bool, error)`
  - `func List(value string) []string`

**Контекст:** эти пять-шесть функций сейчас скопированы в шести `config.go`, причём с расхождениями — `intEnv` в одних местах требует `> 0`, в других `>= 0`. Спека §4.1.

- [ ] **Step 1: Написать падающий тест**

```go
package config

import (
	"testing"
	"time"
)

func TestRequired(t *testing.T) {
	t.Setenv("TEST_KEY", "  value  ")
	got, err := Required("TEST_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "value" {
		t.Errorf("got %q, want %q (must trim)", got, "value")
	}

	t.Setenv("TEST_KEY", "   ")
	if _, err := Required("TEST_KEY"); err == nil {
		t.Error("whitespace-only value must be treated as missing")
	}
}

func TestPositiveIntRejectsZero(t *testing.T) {
	t.Setenv("N", "0")
	if _, err := PositiveInt("N", 5); err == nil {
		t.Error("PositiveInt must reject 0")
	}
}

func TestNonNegativeIntAcceptsZero(t *testing.T) {
	t.Setenv("N", "0")
	got, err := NonNegativeInt("N", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestDurationDefault(t *testing.T) {
	got, err := Duration("MISSING_KEY", 3*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 3*time.Second {
		t.Errorf("got %v, want 3s", got)
	}
}

func TestList(t *testing.T) {
	got := List(" a , ,b ")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %#v, want [a b] with blanks dropped", got)
	}
}
```

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./internal/common/config/ -v`
Expected: FAIL — `undefined: Required`

- [ ] **Step 3: Реализовать**

```go
// Package config reads and validates environment variables.
// It replaces the per-service helpers that had drifted apart:
// intEnv required a positive value in some services and allowed
// zero in others.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func Required(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("missing %s", key)
	}
	return value, nil
}

func Optional(key, def string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	return value
}

func Duration(key string, def time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

func PositiveInt(key string, def int) (int, error) {
	return boundedInt(key, def, 1)
}

func NonNegativeInt(key string, def int) (int, error) {
	return boundedInt(key, def, 0)
}

func boundedInt(key string, def, min int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < min {
		return 0, fmt.Errorf("invalid %s: must be an integer >= %d", key, min)
	}
	return parsed, nil
}

func Bool(key string, def bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s: must be boolean", key)
	}
	return parsed, nil
}

func List(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
```

- [ ] **Step 4: Запустить тест, убедиться что проходит**

Run: `go test ./internal/common/config/ -v`
Expected: PASS, все пять тестов.

- [ ] **Step 5: Commit**

```bash
git add internal/common/config/
git commit -m "feat: add shared config helpers

The six per-service copies had drifted: intEnv rejected zero in some
services and accepted it in others. PositiveInt and NonNegativeInt
make that distinction explicit."
```

---

### Task V5-03: Пакет internal/common/retry — классификация ошибок

**Зависит от:** V5-01. **Блокирует:** V5-08.
**Worktree:** `v5/03-common-retry`

**Files:**
- Create: `internal/common/retry/classify.go`
- Create: `internal/common/retry/classify_test.go`
- Create: `internal/common/retry/sleep.go`

**Interfaces:**
- Produces:
  - `type Kind int` с константами `Transient` и `Permanent`
  - `func Classify(err error) Kind`
  - `func IsTransient(err error) bool`
  - `func Sleep(ctx context.Context, d time.Duration) bool`

**Контекст:** дефект P1-2b из `AUDIT.md`. При сбое Discord API `RecordAttempt` инкрементирует счётчик, и после 10 попыток сообщение уезжает в `discord_message_failed` безвозвратно — хотя причина внешняя и временная. Получасовой сбой Discord выкидывает сообщения из конвейера навсегда.

- [ ] **Step 1: Написать падающий тест**

```go
package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestClassifyTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"deadline", context.DeadlineExceeded},
		{"eof", io.ErrUnexpectedEOF},
		{"net timeout", &net.DNSError{IsTimeout: true}},
		{"discord 500", &discordgo.RESTError{Response: &http.Response{StatusCode: 500}}},
		{"discord 429", &discordgo.RESTError{Response: &http.Response{StatusCode: 429}}},
		{"wrapped", fmt.Errorf("fetch discord message: %w", context.DeadlineExceeded)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != Transient {
				t.Errorf("Classify(%v) = %v, want Transient", tc.err, got)
			}
		})
	}
}

func TestClassifyPermanent(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"discord 404", &discordgo.RESTError{Response: &http.Response{StatusCode: 404}}},
		{"discord 403", &discordgo.RESTError{Response: &http.Response{StatusCode: 403}}},
		{"parse failure", errors.New("message has no embeds")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != Permanent {
				t.Errorf("Classify(%v) = %v, want Permanent", tc.err, got)
			}
		})
	}
}

func TestClassifyNil(t *testing.T) {
	if got := Classify(nil); got != Permanent {
		t.Errorf("Classify(nil) = %v, want Permanent (nil must never be retried)", got)
	}
}

func TestSleepRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Sleep(ctx, time.Hour) {
		t.Error("Sleep must return false when context is already cancelled")
	}
}
```

Добавить `"net/http"` в импорты теста.

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./internal/common/retry/ -v`
Expected: FAIL — `undefined: Classify`

- [ ] **Step 3: Реализовать классификацию**

```go
// Package retry separates failures that will resolve on their own from
// failures that will not.
//
// Without this distinction a thirty-minute Discord outage exhausts the
// ten-attempt budget and drops messages into the failed table for good,
// even though nothing was wrong with the messages themselves.
package retry

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type Kind int

const (
	// Permanent is the zero value on purpose: an unrecognised error is
	// counted as an attempt, so a genuinely broken message still reaches
	// the failed table instead of retrying forever.
	Permanent Kind = iota
	Transient
)

func (k Kind) String() string {
	if k == Transient {
		return "transient"
	}
	return "permanent"
}

func Classify(err error) Kind {
	if err == nil {
		return Permanent
	}

	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return Transient
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return Transient
	}

	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) && restErr.Response != nil {
		code := restErr.Response.StatusCode
		if code == 429 || code >= 500 {
			return Transient
		}
		return Permanent
	}

	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"connection refused",
		"connection reset",
		"no such host",
		"i/o timeout",
		"client.timeout",
		"context deadline exceeded",
		"unexpected eof",
	} {
		if strings.Contains(msg, marker) {
			return Transient
		}
	}

	return Permanent
}

func IsTransient(err error) bool {
	return Classify(err) == Transient
}
```

- [ ] **Step 4: Реализовать Sleep**

```go
package retry

import (
	"context"
	"time"
)

// Sleep waits for d and reports whether the wait completed. It returns
// false as soon as ctx is done. Replaces six copies of sleepWithContext.
func Sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
```

- [ ] **Step 5: Запустить тесты**

Run: `go test ./internal/common/retry/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/common/retry/
git commit -m "feat: classify errors as transient or permanent

A thirty-minute Discord outage used to exhaust the ten-attempt budget
and drop messages into the failed table permanently. Transient failures
no longer count against that budget.

Permanent is the zero value so an unrecognised error still terminates."
```

---

### Task V5-04: Пакет internal/common/health — liveness и readiness

**Зависит от:** V5-01. **Блокирует:** V5-09.
**Worktree:** `v5/04-common-health`

**Files:**
- Create: `internal/common/health/server.go`
- Create: `internal/common/health/server_test.go`

**Interfaces:**
- Produces:
  - `type Check func(ctx context.Context) error`
  - `type Server struct{ ... }`
  - `func NewServer(addr string, log zerolog.Logger) *Server`
  - `func (s *Server) AddLiveness(name string, c Check)`
  - `func (s *Server) AddReadiness(name string, c Check)`
  - `func (s *Server) Start(ctx context.Context)`
  - `func (s *Server) Shutdown(ctx context.Context) error`
  - `func WaitFor(ctx context.Context, name string, attempts int, delay time.Duration, log zerolog.Logger, c Check) error`

**Контекст:** дефект P2-3. Сейчас `/health` у processor'а возвращает 503 при недоступности Discord, autoheal перезапускает контейнер, рестарт ничего не чинит — получается луп. Спека §4.3.

- [ ] **Step 1: Написать падающий тест**

```go
package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func testServer() *Server {
	return NewServer(":0", zerolog.New(os.Stdout))
}

func TestLivenessIgnoresReadinessFailure(t *testing.T) {
	s := testServer()
	s.AddLiveness("db", func(context.Context) error { return nil })
	s.AddReadiness("discord", func(context.Context) error {
		return errors.New("discord unreachable")
	})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/health = %d, want 200: a Discord outage must not restart the container", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/ready = %d, want 503", rec.Code)
	}
}

func TestLivenessFailsWhenOwnDependencyIsDown(t *testing.T) {
	s := testServer()
	s.AddLiveness("db", func(context.Context) error { return errors.New("postgres down") })

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/health = %d, want 503", rec.Code)
	}
}

func TestReadyIncludesLiveness(t *testing.T) {
	s := testServer()
	s.AddLiveness("db", func(context.Context) error { return errors.New("postgres down") })
	s.AddReadiness("discord", func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/ready = %d, want 503 when a liveness check fails", rec.Code)
	}
}
```

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./internal/common/health/ -v`
Expected: FAIL — `undefined: NewServer`

- [ ] **Step 3: Реализовать**

```go
// Package health separates liveness from readiness.
//
// The processor used to report unhealthy whenever the Discord API was
// unreachable, so autoheal restarted a container that had nothing wrong
// with it. Liveness now covers only what a restart could actually fix.
package health

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

type Check func(ctx context.Context) error

type namedCheck struct {
	name  string
	check Check
}

type Server struct {
	addr      string
	log       zerolog.Logger
	liveness  []namedCheck
	readiness []namedCheck
	srv       *http.Server
}

func NewServer(addr string, log zerolog.Logger) *Server {
	return &Server{addr: addr, log: log}
}

func (s *Server) AddLiveness(name string, c Check) {
	s.liveness = append(s.liveness, namedCheck{name, c})
}

func (s *Server) AddReadiness(name string, c Check) {
	s.readiness = append(s.readiness, namedCheck{name, c})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		s.run(w, r, s.liveness)
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		s.run(w, r, append(append([]namedCheck{}, s.liveness...), s.readiness...))
	})
	return mux
}

func (s *Server) run(w http.ResponseWriter, r *http.Request, checks []namedCheck) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	for _, c := range checks {
		if err := c.check(ctx); err != nil {
			s.log.Warn().Err(err).Str("check", c.name).Msg("health check failed")
			http.Error(w, fmt.Sprintf("%s not healthy", c.name), http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) Start(ctx context.Context) {
	s.srv = &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.log.Info().Str("addr", s.addr).Msg("health server listening")

	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error().Err(err).Msg("health server stopped")
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			s.log.Error().Err(err).Msg("health server shutdown error")
		}
	}()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// WaitFor blocks until the check succeeds or attempts run out.
// Replaces waitForPostgres, waitForMongo and waitForMinio.
func WaitFor(ctx context.Context, name string, attempts int, delay time.Duration, log zerolog.Logger, c Check) error {
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		checkCtx, cancel := context.WithTimeout(ctx, delay+5*time.Second)
		lastErr = c(checkCtx)
		cancel()
		if lastErr == nil {
			return nil
		}
		if attempt == attempts {
			break
		}
		log.Warn().Err(lastErr).Str("dependency", name).
			Int("attempt", attempt).Int("max_attempts", attempts).
			Msg("dependency not ready, retrying")
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("%s not ready: %w", name, lastErr)
}
```

- [ ] **Step 4: Запустить тесты**

Run: `go test ./internal/common/health/ -v`
Expected: PASS, все три теста.

- [ ] **Step 5: Commit**

```bash
git add internal/common/health/
git commit -m "feat: split liveness from readiness

An unreachable Discord API used to make the processor report unhealthy,
so autoheal restarted a container that a restart could not fix.
Liveness now covers only the service's own dependencies."
```

---

### Task V5-05: Пакет internal/common/queue — eligibility

**Зависит от:** V5-01. **Блокирует:** V5-07.
**Worktree:** `v5/05-common-queue`

**Files:**
- Create: `internal/common/queue/eligibility.go`
- Create: `internal/common/queue/eligibility_test.go`

**Interfaces:**
- Produces:
  - `const StatusNew = "new"`, `StatusInProgress = "in_progress"`, `StatusProcessed = "processed"`
  - `func StaleThreshold(retryInterval time.Duration) time.Duration`
  - `func StaleCutoff(now time.Time, retryInterval time.Duration) time.Time`

**Контекст:** дефект P1-3. `PROCESS_RETRY_INTERVAL` служит одновременно периодом тикера и порогом eligibility, а в SQL стоит нестрогое `<=`. Отправка длительностью ровно 60 с подхватывается повторно, и пост уходит в Telegram дважды. HTTP-таймаут Telegram — 2 минуты, то есть сценарий рабочий. Спека §4.3.

- [ ] **Step 1: Написать падающий тест**

```go
package queue

import (
	"testing"
	"time"
)

func TestStaleThresholdIsLargerThanTickerInterval(t *testing.T) {
	interval := time.Minute
	got := StaleThreshold(interval)
	if got <= interval {
		t.Fatalf("StaleThreshold(%v) = %v; must exceed the ticker interval, "+
			"otherwise a send that takes exactly one interval is picked up twice", interval, got)
	}
	if got != 3*time.Minute {
		t.Errorf("StaleThreshold(%v) = %v, want 3m", interval, got)
	}
}

func TestStaleCutoffExcludesRecentAttempt(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	cutoff := StaleCutoff(now, time.Minute)

	startedOneIntervalAgo := now.Add(-time.Minute)
	if !startedOneIntervalAgo.After(cutoff) {
		t.Error("an attempt started one interval ago must NOT be eligible for retry")
	}

	startedLongAgo := now.Add(-10 * time.Minute)
	if !startedLongAgo.Before(cutoff) {
		t.Error("an attempt started ten intervals ago must be eligible for retry")
	}
}

func TestStaleThresholdZeroInterval(t *testing.T) {
	if got := StaleThreshold(0); got != 0 {
		t.Errorf("StaleThreshold(0) = %v, want 0 (retry loop disabled)", got)
	}
}
```

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./internal/common/queue/ -v`
Expected: FAIL — `undefined: StaleThreshold`

- [ ] **Step 3: Реализовать**

```go
// Package queue holds the retry-eligibility rules shared by every
// service that owns a status table.
//
// The rule used to be "retry anything whose last attempt is older than
// PROCESS_RETRY_INTERVAL", with the same value driving the ticker and
// a non-strict <= in SQL. A Telegram send that took exactly one
// interval was therefore picked up a second time and the post went out
// twice. The threshold is now a multiple of the ticker interval.
package queue

import "time"

const (
	StatusNew        = "new"
	StatusInProgress = "in_progress"
	StatusProcessed  = "processed"
)

// staleFactor keeps the eligibility threshold clear of the ticker
// interval. Telegram's HTTP client allows two minutes per request, so
// with a one-minute ticker the threshold must exceed that.
const staleFactor = 3

func StaleThreshold(retryInterval time.Duration) time.Duration {
	if retryInterval <= 0 {
		return 0
	}
	return retryInterval * staleFactor
}

func StaleCutoff(now time.Time, retryInterval time.Duration) time.Time {
	return now.Add(-StaleThreshold(retryInterval))
}
```

- [ ] **Step 4: Запустить тесты**

Run: `go test ./internal/common/queue/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/common/queue/
git commit -m "feat: add shared retry-eligibility rules

The ticker interval doubled as the eligibility threshold with a
non-strict comparison, so a Telegram send lasting exactly one interval
was retried while still in flight and the post went out twice."
```

---

Продолжение плана — фазы 1–5 — в файле `2026-07-28-mega-games-v5-part2.md`.
