# MEGA Games Bot v5.0 — План, часть 2: фазы 1–2

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Продолжение `2026-07-28-mega-games-v5.md`. Global Constraints — см. часть 1.

---

# Фаза 1 — Тестовый фундамент

### Task V5-06: Харнесс testcontainers для Postgres

**Зависит от:** V5-01. **Блокирует:** V5-07, V5-14, V5-22.
**Worktree:** `v5/06-pgtest`

**Files:**
- Create: `internal/common/pgtest/pgtest.go`
- Modify: `go.mod` (добавить testcontainers)
- Modify: `.github/workflows/publish-images.yml` (добавить job `test`)

**Interfaces:**
- Produces:
  - `func NewPostgres(t *testing.T) *pgxpool.Pool` — поднимает контейнер, возвращает пул, регистрирует очистку через `t.Cleanup`
  - `func ApplySchema(t *testing.T, pool *pgxpool.Pool, ddl ...string)`

**Контекст:** логика статусов и eligibility — это SQL. Мок проверил бы фантазию автора, а не поведение `ON CONFLICT`, `FOR UPDATE` и границ сравнения. Именно здесь сидит P1-3. Спека §5.

- [ ] **Step 1: Добавить зависимости**

```bash
go get github.com/testcontainers/testcontainers-go@v0.43.0
go get github.com/testcontainers/testcontainers-go/modules/postgres@v0.43.0
go mod tidy
```

- [ ] **Step 2: Реализовать харнесс**

```go
// Package pgtest starts a throwaway PostgreSQL for integration tests.
//
// The status tables carry the correctness of the whole pipeline —
// idempotent inserts, attempt counting, the transaction behind
// MoveToFailed. Those are SQL behaviours; a fake would only assert
// what the author already believed.
package pgtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}

	ctx := context.Background()
	container, err := postgres.Run(ctx,
		"postgres:16.4-alpine",
		postgres.WithDatabase("megagames_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	return pool
}

func ApplySchema(t *testing.T, pool *pgxpool.Pool, ddl ...string) {
	t.Helper()
	for _, stmt := range ddl {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			t.Fatalf("apply schema: %v\nstatement: %s", err, stmt)
		}
	}
}
```

- [ ] **Step 3: Проверить, что харнесс поднимается**

Написать временный smoke-тест `internal/common/pgtest/smoke_test.go`:

```go
package pgtest

import (
	"context"
	"testing"
)

func TestPostgresStarts(t *testing.T) {
	pool := NewPostgres(t)
	var one int
	if err := pool.QueryRow(context.Background(), "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("query: %v", err)
	}
	if one != 1 {
		t.Errorf("got %d, want 1", one)
	}
}
```

Run: `go test ./internal/common/pgtest/ -v`
Expected: PASS (первый прогон тянет образ, до минуты).

Run: `go test ./internal/common/pgtest/ -short -v`
Expected: SKIP.

- [ ] **Step 4: Добавить job тестов в CI**

В `.github/workflows/publish-images.yml` добавить перед job `publish`:

```yaml
  test:
    name: test
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v6

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.25.5'
          cache: true

      - name: Vet
        run: go vet ./...

      - name: Test
        run: go test ./... -race -timeout 15m
```

И добавить в job `publish`:

```yaml
    needs: test
```

Docker в `ubuntu-latest` доступен из коробки, отдельная настройка для testcontainers не нужна.

- [ ] **Step 5: Commit**

```bash
git add internal/common/pgtest/ go.mod go.sum .github/workflows/publish-images.yml
git commit -m "test: add throwaway Postgres harness and CI test job

Status-table behaviour is SQL behaviour: ON CONFLICT, transaction
boundaries, comparison operators. A fake would assert the author's
assumptions rather than what the database does.

-short skips container tests for a fast local loop."
```

---

# Фаза 2 — Дефекты

Задачи V5-07 … V5-17 трогают непересекающиеся файлы и выполняются параллельно, кроме V5-15 (после V5-14).

### Task V5-07: Применить eligibility во всех сервисах — фикс дублей в Telegram

**Зависит от:** V5-05, V5-06. **Параллельно с:** V5-08…V5-17.
**Worktree:** `v5/07-eligibility`

**Files:**
- Modify: `discord_kafka_telegram_week_sender/internal/store/store.go:103-155`
- Modify: `discord_kafka_telegram_week_sender/internal/processor/processor.go:127-131`
- Modify: `discord_kafka_telegram_game_sender/internal/store/store.go:104-155`
- Modify: `discord_kafka_game_image/internal/pgstore/store.go:108-131`
- Create: `discord_kafka_telegram_week_sender/internal/store/store_test.go`
- **Не трогает:** processor.go game_image, week_formatter, listener, admin

**Interfaces:**
- Consumes: `queue.StaleThreshold`, `queue.StatusInProgress`, `pgtest.NewPostgres`
- Produces: `func (s *Store) TouchAttempt(ctx context.Context, messageID string, retryInterval time.Duration) error` в week-sender (сигнатура выравнивается с game-sender — сейчас у week-sender нет параметра)

**Контекст:** P1-3. У `telegram-week-sender` защиты нет вовсе: `TouchAttempt` только обновляет `last_attempt_at`, не переводя в `in_progress`, а `consumeLoop` не проверяет `in_progress`. У остальных защита есть, но граничное условие `<=` с порогом, равным интервалу тикера, всё равно допускает дубль.

- [ ] **Step 1: Написать падающий тест на week-sender**

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/colzphml/mega_games/internal/common/pgtest"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	pool := pgtest.NewPostgres(t)
	s := &Store{pool: pool, log: zerolog.Nop()}
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return s
}

func TestInFlightMessageIsNotRetried(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute

	if _, err := s.EnsureMessage(ctx, "msg-1", "hello"); err != nil {
		t.Fatalf("ensure message: %v", err)
	}

	// Отправка началась: сообщение переходит в in_progress.
	if err := s.TouchAttempt(ctx, "msg-1", retryInterval); err != nil {
		t.Fatalf("touch attempt: %v", err)
	}

	// Ретрай-луп срабатывает, пока отправка ещё идёт.
	pending, err := s.ListPending(ctx, 100, retryInterval)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("got %d pending, want 0: an in-flight send must not be "+
			"retried, otherwise the post goes to Telegram twice", len(pending))
	}
}

func TestStuckMessageIsRetriedEventually(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute

	if _, err := s.EnsureMessage(ctx, "msg-2", "hello"); err != nil {
		t.Fatalf("ensure message: %v", err)
	}
	if err := s.TouchAttempt(ctx, "msg-2", retryInterval); err != nil {
		t.Fatalf("touch attempt: %v", err)
	}

	// Сдвигаем last_attempt_at за порог устаревания (3 x интервал).
	if _, err := s.pool.Exec(ctx,
		`UPDATE telegram_week_status SET last_attempt_at = NOW() - INTERVAL '10 minutes'
		 WHERE message_id = $1`, "msg-2"); err != nil {
		t.Fatalf("age the row: %v", err)
	}

	pending, err := s.ListPending(ctx, 100, retryInterval)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("got %d pending, want 1: a send stuck past the threshold "+
			"must be retried", len(pending))
	}
}

func TestTouchAttemptRejectsInFlightMessage(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	retryInterval := time.Minute

	if _, err := s.EnsureMessage(ctx, "msg-3", "hello"); err != nil {
		t.Fatalf("ensure message: %v", err)
	}
	if err := s.TouchAttempt(ctx, "msg-3", retryInterval); err != nil {
		t.Fatalf("first touch: %v", err)
	}
	if err := s.TouchAttempt(ctx, "msg-3", retryInterval); err == nil {
		t.Error("second TouchAttempt must fail while the first is in flight")
	}
}
```

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./discord_kafka_telegram_week_sender/internal/store/ -run TestInFlight -v`
Expected: FAIL — `TouchAttempt` принимает два аргумента, не три; и сообщение остаётся в статусе `new`, поэтому `ListPending` его вернёт.

- [ ] **Step 3: Переписать TouchAttempt в week-sender**

Заменить в `discord_kafka_telegram_week_sender/internal/store/store.go`:

```go
// TouchAttempt claims a message for processing. It fails when another
// worker is already sending it, which is what keeps a post from going
// to Telegram twice.
func (s *Store) TouchAttempt(ctx context.Context, messageID string, retryInterval time.Duration) error {
	cutoff := queue.StaleCutoff(time.Now(), retryInterval)
	res, err := s.pool.Exec(
		ctx,
		`UPDATE telegram_week_status
		 SET status = $2, last_attempt_at = NOW(), updated_at = NOW()
		 WHERE message_id = $1
		   AND (
			status = $3
			OR (status = $2 AND last_attempt_at < $4)
		   )`,
		messageID,
		queue.StatusInProgress,
		queue.StatusNew,
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("touch attempt: %w", err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("touch attempt: message not eligible")
	}
	return nil
}
```

Добавить импорт `"github.com/colzphml/mega_games/internal/common/queue"`.

- [ ] **Step 4: Переписать ListPending в week-sender**

```go
func (s *Store) ListPending(ctx context.Context, limit int, retryInterval time.Duration) ([]WeekMessage, error) {
	if limit <= 0 {
		limit = 1000
	}
	var rows pgx.Rows
	var err error
	if retryInterval > 0 {
		cutoff := queue.StaleCutoff(time.Now(), retryInterval)
		rows, err = s.pool.Query(ctx,
			`SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload
			 FROM telegram_week_status
			 WHERE (status = $1 AND (last_attempt_at IS NULL OR last_attempt_at < $3))
			    OR (status = $2 AND last_attempt_at < $3)
			 ORDER BY created_at ASC LIMIT $4`,
			queue.StatusNew, queue.StatusInProgress, cutoff, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload
			 FROM telegram_week_status
			 WHERE status = $1 ORDER BY created_at ASC LIMIT $2`,
			queue.StatusNew, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()

	var messages []WeekMessage
	for rows.Next() {
		var msg WeekMessage
		if err := rows.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt, &msg.LastError, &msg.LastAttempt, &msg.Payload); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}
```

Добавить `func StatusInProgress() string { return queue.StatusInProgress }` в конец файла.

- [ ] **Step 5: Обновить processor week-sender**

В `discord_kafka_telegram_week_sender/internal/processor/processor.go` заменить в `processMessage`:

```go
func (p *Processor) processMessage(ctx context.Context, msg store.WeekMessage, details map[string]any) {
	// A failed claim means another worker holds this message. Skipping is
	// the point: it is what stops a duplicate post.
	if err := p.store.TouchAttempt(ctx, msg.ID, p.cfg.ProcessRetryInterval); err != nil {
		p.log.Info().Err(err).Str("message_id", msg.ID).Msg("week message not eligible for processing")
		return
	}
	if err := p.handleMessage(ctx, msg); err != nil {
```

Далее в `consumeLoop` после проверки `StatusProcessed` добавить:

```go
			if stored.Status == store.StatusInProgress() {
				p.log.Info().Str("message_id", messageID).Msg("week message already in progress")
				continue
			}
```

- [ ] **Step 6: Запустить тесты week-sender**

Run: `go test ./discord_kafka_telegram_week_sender/... -v`
Expected: PASS, все три теста.

- [ ] **Step 7: Заменить `<=` на `<` в game-sender и pgstore**

В `discord_kafka_telegram_game_sender/internal/store/store.go` в `ListPending` и `TouchAttempt` заменить `last_attempt_at <= $N` на `last_attempt_at < $N`, а вычисление `cutoff := time.Now().Add(-retryAfter)` на `cutoff := queue.StaleCutoff(time.Now(), retryAfter)`.

То же самое в `discord_kafka_game_image/internal/pgstore/store.go` в `TouchAttempt` (строки 108–131).

- [ ] **Step 8: Проверить сборку и прогнать всё**

Run: `go build ./... && go test ./... -short`
Expected: успех.

- [ ] **Step 9: Commit**

```bash
git add discord_kafka_telegram_week_sender/ discord_kafka_telegram_game_sender/ discord_kafka_game_image/internal/pgstore/
git commit -m "fix: stop duplicate Telegram posts on slow sends

The week sender had no in_progress guard at all, and everywhere else
the eligibility threshold equalled the ticker interval with a
non-strict comparison. A send lasting exactly one interval was claimed
a second time while still in flight; Telegram's client allows two
minutes per request, so this was reachable in practice."
```

---

### Task V5-08: Классификация ошибок Discord в processor

**Зависит от:** V5-03. **Параллельно с:** V5-07, V5-09…V5-17.
**Worktree:** `v5/08-discord-errors`

**Files:**
- Modify: `discord_kafka_processor/internal/processor/processor.go:139-154, 212-249`
- Create: `discord_kafka_processor/internal/processor/classify_test.go`
- **Не трогает:** store, config, discord/client.go

**Interfaces:**
- Consumes: `retry.Classify`, `retry.Kind`, `retry.Transient`, `retry.Sleep`
- Produces: поведение `processMessage` — при `retry.Transient` счётчик попыток не увеличивается

**Контекст:** P1-2b. Сбой Discord на 30 минут исчерпывает 10 попыток и отправляет сообщения в `discord_message_failed` навсегда.

- [ ] **Step 1: Написать падающий тест**

```go
package processor

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/colzphml/mega_games/internal/common/retry"
)

// fakeAttemptRecorder counts how many times a failure was charged
// against the retry budget.
type fakeAttemptRecorder struct {
	attempts int
}

func (f *fakeAttemptRecorder) record() { f.attempts++ }

func TestTransientFailuresDoNotConsumeAttempts(t *testing.T) {
	rec := &fakeAttemptRecorder{}
	outage := &discordgo.RESTError{Response: &http.Response{StatusCode: 503}}

	for i := 0; i < 20; i++ {
		if retry.Classify(outage) != retry.Transient {
			t.Fatal("a 503 from Discord must be transient")
		}
		// shouldCharge mirrors the decision processMessage makes.
		if shouldChargeAttempt(outage) {
			rec.record()
		}
	}

	if rec.attempts != 0 {
		t.Errorf("charged %d attempts during an outage, want 0: a 30-minute "+
			"Discord outage must not exhaust the budget and drop messages", rec.attempts)
	}
}

func TestPermanentFailureConsumesAttempt(t *testing.T) {
	rec := &fakeAttemptRecorder{}
	gone := &discordgo.RESTError{Response: &http.Response{StatusCode: 404}}

	if shouldChargeAttempt(gone) {
		rec.record()
	}
	if rec.attempts != 1 {
		t.Errorf("charged %d attempts for a 404, want 1", rec.attempts)
	}
}

func TestParseFailureConsumesAttempt(t *testing.T) {
	if !shouldChargeAttempt(errors.New("message has no embeds")) {
		t.Error("an unparseable message must consume an attempt")
	}
}

var _ = context.Background
```

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./discord_kafka_processor/internal/processor/ -run TestTransient -v`
Expected: FAIL — `undefined: shouldChargeAttempt`

- [ ] **Step 3: Реализовать**

В `processor.go` добавить:

```go
// shouldChargeAttempt reports whether a failure counts against
// PROCESS_MAX_ATTEMPTS. Transient failures do not: otherwise a
// prolonged Discord outage would exhaust the budget and move perfectly
// good messages to the failed table for good.
func shouldChargeAttempt(err error) bool {
	return retry.Classify(err) != retry.Transient
}
```

Заменить `processMessage`:

```go
func (p *Processor) processMessage(ctx context.Context, messageID string, details map[string]any) {
	err := p.handleMessage(ctx, messageID)
	if err == nil {
		return
	}

	if !shouldChargeAttempt(err) {
		p.log.Warn().Err(err).Str("message_id", messageID).
			Str("error_kind", retry.Classify(err).String()).
			Msg("transient failure, message stays queued without consuming an attempt")
		return
	}

	updated, updateErr := p.store.RecordAttempt(ctx, messageID, err.Error())
	if updateErr != nil {
		p.log.Error().Err(updateErr).Str("message_id", messageID).Msg("failed to record processing attempt")
		return
	}
	if updated.Attempts >= p.cfg.MaxAttempts {
		if moveErr := p.store.MoveToFailed(ctx, messageID, details); moveErr != nil {
			p.log.Error().Err(moveErr).Str("message_id", messageID).Msg("failed to move message to failed table")
		}
		return
	}
	p.log.Error().Err(err).Str("message_id", messageID).Msg("failed to process message")
}
```

Добавить импорт `"github.com/colzphml/mega_games/internal/common/retry"`.

- [ ] **Step 4: Заменить sleepWithContext на retry.Sleep**

В `fetchAndParse` заменить вызовы `sleepWithContext(ctx, p.cfg.FetchDelay)` на `retry.Sleep(ctx, p.cfg.FetchDelay)` и удалить локальную функцию `sleepWithContext` из `processor.go`.

- [ ] **Step 5: Запустить тесты**

Run: `go test ./discord_kafka_processor/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add discord_kafka_processor/internal/processor/
git commit -m "fix: do not charge transient Discord failures against retries

A thirty-minute Discord outage burned all ten attempts and moved
messages to discord_message_failed permanently, even though nothing was
wrong with them. Timeouts, 5xx and 429 now leave the budget untouched;
404 and parse failures still consume it."
```

---

### Task V5-09: Разделить /health и /ready во всех сервисах

**Зависит от:** V5-04. **Параллельно с:** V5-07, V5-08, V5-10…V5-17.
**Worktree:** `v5/09-health-split`

**Files:**
- Modify: `discord_kafka_processor/cmd/discord-kafka-processor/main.go:105-146`
- Modify: `discord_kafka_listener/cmd/discord-kafka-listener/main.go:79-93`
- Modify: `discord_kafka_week_formatter/cmd/discord-kafka-week-formatter/main.go:100-141`
- Modify: `discord_kafka_game_image/cmd/discord-kafka-game-image/main.go:132-181`
- Modify: `discord_kafka_telegram_game_sender/cmd/discord-kafka-telegram-game-sender/main.go:107-152`
- Modify: `discord_kafka_telegram_week_sender/cmd/discord-kafka-telegram-week-sender/main.go`
- **Не трогает:** internal/ любого сервиса

**Interfaces:**
- Consumes: `health.NewServer`, `health.Check`, `health.WaitFor`

**Контекст:** P2-3. Спека §4.3.

- [ ] **Step 1: Переписать health-сервер processor'а**

В `discord_kafka_processor/cmd/discord-kafka-processor/main.go` удалить функции `startHealthServer` и `waitForPostgres`, заменить их использование:

```go
	healthSrv := health.NewServer(cfg.HealthAddr, log)
	healthSrv.AddLiveness("postgres", storeClient.Ping)
	// Discord is a readiness concern only: restarting this container
	// cannot fix an outage on Discord's side, and autoheal watches
	// /health, so listing it there caused restart loops.
	healthSrv.AddReadiness("discord", func(ctx context.Context) error {
		if !discordClient.Healthy() {
			return errors.New("discord api unreachable")
		}
		return nil
	})
	healthSrv.Start(ctx)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = healthSrv.Shutdown(shutdownCtx)
	}()
```

И заменить ожидание Postgres:

```go
	if err := health.WaitFor(ctx, "postgres", cfg.PostgresMaxAttempts, cfg.PostgresRetryDelay, log, storeClient.Ping); err != nil {
		log.Fatal().Err(err).Msg("postgres not ready")
	}
```

Добавить импорты `"errors"` и `"github.com/colzphml/mega_games/internal/common/health"`, удалить `"net/http"` если больше не используется.

- [ ] **Step 2: Повторить для остальных пяти сервисов**

Правила распределения проверок:

| Сервис | Liveness | Readiness |
|---|---|---|
| listener | — (только HTTP-сервер жив) | discord ready |
| processor | postgres | discord |
| week-formatter | postgres, kafka | — |
| game-image | postgres, minio, kafka | — |
| telegram-week-sender | postgres, kafka | — |
| telegram-game-sender | postgres, minio, kafka | — |

Для listener liveness остаётся пустым — сервис жив, если HTTP отвечает; готовность к работе определяется состоянием сессии Discord.

Для сервисов с Kafka заменить локальную `kafkaPing` на общую проверку:

```go
	healthSrv.AddLiveness("kafka", func(ctx context.Context) error {
		dialer := &net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", cfg.KafkaBrokers[0])
		if err != nil {
			return err
		}
		return conn.Close()
	})
```

Удалить дублирующиеся `kafkaPing`, `waitForPostgres`, `waitForMongo`, `waitForMinio`, `sleepWithContext` из всех шести `main.go`.

- [ ] **Step 3: Обновить healthcheck в compose**

В `docker-compose.yml` у всех шести сервисов оставить `/health` (autoheal должен реагировать только на liveness) и увеличить интервал:

```yaml
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 5
      start_period: 30s
```

Для `admin-panel` — порт 8081. Для `postgres` и `minio` интервал тоже поднять до 30s.

- [ ] **Step 4: Проверить сборку**

Run: `go build ./... && go vet ./...`
Expected: успех, пустой вывод vet.

- [ ] **Step 5: Проверить руками**

```bash
docker compose up -d --build discord-kafka-processor
docker compose exec -T discord-kafka-processor wget -qO- http://127.0.0.1:8080/health
docker compose exec -T discord-kafka-processor wget -qO- http://127.0.0.1:8080/ready
```
Expected: оба отвечают `ok`.

- [ ] **Step 6: Commit**

```bash
git add discord_kafka_*/cmd/ docker-compose.yml
git commit -m "fix: separate liveness from readiness

Discord being unreachable made the processor report unhealthy, so
autoheal restarted a container that a restart could not fix. Liveness
now covers only dependencies the service owns; Discord moved to
/ready, which autoheal does not watch.

Health check intervals go from 5-10s to 30s: this pipeline runs in
weekly batches, and pg_isready forks a process every time."
```

---

### Task V5-10: Постсезон и формат поста

**Зависит от:** V5-01. **Параллельно с:** V5-07…V5-09, V5-11…V5-17.
**Worktree:** `v5/10-postseason`

**Files:**
- Modify: `discord_kafka_week_formatter/internal/store/store.go:252-287`
- Modify: `discord_kafka_week_formatter/internal/formatter/formatter.go:11-56`
- Create: `discord_kafka_week_formatter/internal/formatter/formatter_test.go`
- **Не трогает:** processor, senders, game_image

**Interfaces:**
- Produces: `func BuildWeekMessage(payload store.WeekPayload, teams map[string]store.Team, games []store.Game) (string, error)` — сигнатура не меняется; для `payload.Season == "postseason"` возвращает пост с пояснением вместо списка матчей

**Контекст:** P1-1. `stageIndex` в `MEGA_games.csv` всегда `1`, плейофф идёт продолжением нумерации недель. Источника расписания плейоффа не существует: регулярка выгружается разово, плейофф не выгружается вовсе. Спека §4.3.

- [ ] **Step 1: Написать падающий тест**

```go
package formatter

import (
	"strings"
	"testing"

	"github.com/colzphml/mega_games/discord_kafka_week_formatter/internal/store"
)

func testTeams() map[string]store.Team {
	return map[string]store.Team{
		"Falcons":    {Name: "Falcons", ShortName: "ATL", Player: "alice"},
		"Buccaneers": {Name: "Buccaneers", ShortName: "TB", Player: "CPU"},
	}
}

func TestPostseasonExplainsMissingSchedule(t *testing.T) {
	got, err := BuildWeekMessage(
		store.WeekPayload{Season: "postseason", Week: 1},
		testTeams(),
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "Игра плейофф, расписание см. в игре") {
		t.Errorf("postseason post must explain why there is no match list, got:\n%s", got)
	}
	if !strings.Contains(got, "Post Season Week 1") {
		t.Errorf("postseason post must keep the heading, got:\n%s", got)
	}
	if !strings.Contains(got, "договоритесь") {
		t.Errorf("postseason post must keep the deadline footer, got:\n%s", got)
	}
}

func TestRegularSeasonListsGames(t *testing.T) {
	got, err := BuildWeekMessage(
		store.WeekPayload{Season: "regular", Week: 3},
		testTeams(),
		[]store.Game{{Home: "Falcons", Away: "Buccaneers"}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "TB(CPU) @ [ATL](t.me/alice)") {
		t.Errorf("regular season post must list games, got:\n%s", got)
	}
	if strings.Contains(got, "плейофф") {
		t.Errorf("regular season post must not mention playoffs, got:\n%s", got)
	}
}

func TestPreseasonWeekFourHasNoGames(t *testing.T) {
	got, err := BuildWeekMessage(
		store.WeekPayload{Season: "preseason", Week: 4},
		testTeams(),
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "Pre Season Week 4") {
		t.Errorf("got:\n%s", got)
	}
}
```

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./discord_kafka_week_formatter/internal/formatter/ -v`
Expected: FAIL — пост для постсезона не содержит пояснения.

- [ ] **Step 3: Реализовать в formatter.go**

```go
// postseasonNote replaces the match list for playoff weeks.
//
// There is no source for a playoff schedule: the regular season is
// exported once into schedule_games, and the playoff bracket is never
// exported at all. An empty list would look like a bug; this says so
// plainly.
const postseasonNote = "Игра плейофф, расписание см. в игре"

func BuildWeekMessage(payload store.WeekPayload, teams map[string]store.Team, games []store.Game) (string, error) {
	if payload.Season == "" {
		return "", fmt.Errorf("invalid week payload")
	}

	title := ""
	if payload.Week > 0 {
		title = fmt.Sprintf("MEGA has advanced to %s Week %d", seasonTitle(payload.Season), payload.Week)
	} else if payload.Title != "" {
		title = payload.Title
	} else {
		return "", fmt.Errorf("invalid week payload")
	}

	var textBuilder strings.Builder
	textBuilder.WriteString(fmt.Sprintf("*%s*\n", title))

	if payload.Season == "postseason" {
		textBuilder.WriteString("\n" + postseasonNote + "\n")
	} else {
		for _, game := range games {
			away, awayOK := teams[game.Away]
			home, homeOK := teams[game.Home]
			if !awayOK || !homeOK {
				return "", fmt.Errorf("missing team data for %s @ %s", game.Away, game.Home)
			}
			textBuilder.WriteString(formatGameLine(away, home))
		}
	}

	base := textBuilder.String()
	deadline := time.Now().In(time.Local).Add(announceDeadline).Format("02 Jan 2006 15:04")
	return fmt.Sprintf("%s\n\n_❗️Пожалуйста, договоритесь прямо сейчас о матче во избежание затяжек шага.\n\nДо %s просьба указать анонс матча реплаем к этому посту_", base, deadline), nil
}

func formatGameLine(away, home store.Team) string {
	awayCPU := isCPU(away.Player)
	homeCPU := isCPU(home.Player)
	switch {
	case awayCPU && homeCPU:
		return fmt.Sprintf("\n%s(CPU) @ %s(CPU)", away.ShortName, home.ShortName)
	case awayCPU:
		return fmt.Sprintf("\n%s(CPU) @ [%s](%s)", away.ShortName, home.ShortName, telegramLink(home.Player))
	case homeCPU:
		return fmt.Sprintf("\n[%s](%s) @ %s(CPU)", away.ShortName, telegramLink(away.Player), home.ShortName)
	default:
		return fmt.Sprintf("\n[%s](%s) @ [%s](%s)", away.ShortName, telegramLink(away.Player), home.ShortName, telegramLink(home.Player))
	}
}
```

Добавить в начало файла `var announceDeadline = 40 * time.Hour` (в V5-27 станет конфигурируемым).

- [ ] **Step 4: Задокументировать выбор сезона в store.go**

Заменить `LoadGamesForWeek`:

```go
// LoadGamesForWeek returns the scheduled games for a week.
//
// The season is taken as MAX(season_index) rather than from the payload
// because a Discord message carries only the season *type*
// ("regular"/"preseason"/"postseason"), never a season number. The
// schedule is loaded once per season, so the newest one in the table is
// the current one — and the pipeline does not depend on having seen the
// season-change message.
//
// Postseason returns nothing: no playoff schedule is ever exported, and
// the formatter prints an explanation instead of an empty list.
func (s *Store) LoadGamesForWeek(ctx context.Context, season string, week int) ([]Game, error) {
	if week < 1 || season == "postseason" {
		return nil, nil
	}
	if season == "preseason" && week == 4 {
		// Preseason week 4 has no scheduled games in this league.
		return nil, nil
	}

	var seasonIndex int
	row := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(season_index), -1) FROM schedule_games`)
	if err := row.Scan(&seasonIndex); err != nil {
		return nil, fmt.Errorf("load max season: %w", err)
	}
	if seasonIndex < 0 {
		return nil, nil
	}
	s.log.Debug().Int("season_index", seasonIndex).Str("season", season).Int("week", week).
		Msg("loading schedule")

	rows, err := s.pool.Query(ctx,
		`SELECT home_team, away_team FROM schedule_games
		 WHERE season_index = $1 AND stage = $2 AND week_index = $3`,
		seasonIndex, true, week-1)
	if err != nil {
		return nil, fmt.Errorf("load games: %w", err)
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		var game Game
		if err := rows.Scan(&game.Home, &game.Away); err != nil {
			return nil, fmt.Errorf("scan game: %w", err)
		}
		games = append(games, game)
	}
	return games, rows.Err()
}
```

`stage` фиксируется в `true`, потому что в экспорте NeonSportz `stageIndex` всегда `1`.

- [ ] **Step 5: Запустить тесты**

Run: `go test ./discord_kafka_week_formatter/... -v`
Expected: PASS, все три теста.

- [ ] **Step 6: Commit**

```bash
git add discord_kafka_week_formatter/
git commit -m "fix: explain the missing playoff schedule instead of printing nothing

stageIndex is always 1 in the NeonSportz export and playoff weeks
continue the regular numbering, so a postseason lookup silently matched
nothing and the post went out with an empty match list.

No playoff schedule is ever exported, so the post now says so."
```

---

### Task V5-11: Экранирование Markdown в Telegram

**Зависит от:** V5-01. **Параллельно с:** V5-07…V5-10, V5-12…V5-17.
**Worktree:** `v5/11-markdown-escape`

**Files:**
- Create: `internal/common/tgmarkdown/escape.go`
- Create: `internal/common/tgmarkdown/escape_test.go`
- Modify: `discord_kafka_week_formatter/internal/formatter/formatter.go` (применить к именам команд и никам)
- **Не трогает:** senders, store

**Interfaces:**
- Produces: `func Escape(s string) string` — экранирует `_ * [ ] ( ) ~ \` > # + - = | { } . !` для Telegram Markdown

**Контекст:** P1-7. `ParseMode = "Markdown"` со склейкой `*%s*` и `[%s](%s)` из данных БД. Символ `_` в нике игрока даст HTTP 400, десять ретраев и перенос в `telegram_week_failed`.

- [ ] **Step 1: Написать падающий тест**

```go
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
```

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./internal/common/tgmarkdown/ -v`
Expected: FAIL — `undefined: Escape`

- [ ] **Step 3: Реализовать**

```go
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

func Escape(s string) string {
	return escaper.Replace(s)
}
```

- [ ] **Step 4: Применить в formatter**

В `formatGameLine` и заголовке обернуть подставляемые значения:

```go
func formatGameLine(away, home store.Team) string {
	awayName := tgmarkdown.Escape(away.ShortName)
	homeName := tgmarkdown.Escape(home.ShortName)
	awayCPU := isCPU(away.Player)
	homeCPU := isCPU(home.Player)
	switch {
	case awayCPU && homeCPU:
		return fmt.Sprintf("\n%s(CPU) @ %s(CPU)", awayName, homeName)
	case awayCPU:
		return fmt.Sprintf("\n%s(CPU) @ [%s](%s)", awayName, homeName, telegramLink(home.Player))
	case homeCPU:
		return fmt.Sprintf("\n[%s](%s) @ %s(CPU)", awayName, telegramLink(away.Player), homeName)
	default:
		return fmt.Sprintf("\n[%s](%s) @ [%s](%s)", awayName, telegramLink(away.Player), homeName, telegramLink(home.Player))
	}
}
```

URL внутри `(...)` **не экранируется** — Telegram ожидает там сырой адрес.

- [ ] **Step 5: Добавить тест в formatter**

```go
func TestGameLineEscapesNickname(t *testing.T) {
	teams := map[string]store.Team{
		"A": {Name: "A", ShortName: "A_TEAM", Player: "some_user"},
		"B": {Name: "B", ShortName: "B", Player: "CPU"},
	}
	got, err := BuildWeekMessage(
		store.WeekPayload{Season: "regular", Week: 1},
		teams,
		[]store.Game{{Home: "A", Away: "B"}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `A\_TEAM`) {
		t.Errorf("team name must be escaped, got:\n%s", got)
	}
	if !strings.Contains(got, "t.me/some_user") {
		t.Errorf("URL must stay raw, got:\n%s", got)
	}
}
```

- [ ] **Step 6: Запустить тесты**

Run: `go test ./internal/common/tgmarkdown/ ./discord_kafka_week_formatter/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/common/tgmarkdown/ discord_kafka_week_formatter/
git commit -m "fix: escape Markdown in team names and nicknames

Values from the database went straight into *bold* and [link] syntax.
A nickname containing an underscore made Telegram reject the message,
which burned ten retries and moved the post to the failed table.

URLs stay raw: Telegram expects an unescaped address inside (...)."
```

---

### Task V5-12: Пометка fallback-карточек gochrome

**Зависит от:** V5-01. **Параллельно с:** V5-07…V5-11, V5-13…V5-17.
**Worktree:** `v5/12-fallback-flag`

**Files:**
- Modify: `discord_kafka_game_image/internal/types/types.go`
- Modify: `discord_kafka_game_image/internal/middle/gochrome/gochrome.go:97-143, 161-175`
- Modify: `discord_kafka_game_image/internal/processor/processor.go:207-241`
- Modify: `cmd/admin_panel/templates/dashboard.html`
- Modify: `internal/admin/store.go` (прочитать признак из JSON)
- **Не трогает:** headless, selenium, senders

**Interfaces:**
- Produces: `types.Result` получает поле `Degraded bool`; в метаданные пишется `Fetcher: "gochrome-fallback"` вместо `"gochrome"`

**Контекст:** P1-4. При падении скриншота возвращается заглушка ~100 КБ, но в метаданные пишется тот же `fetcher: "gochrome"`. Отличить можно только по размеру файла. Именно так сломанный после редизайна DOM неделями выглядел как «всё зелёное».

- [ ] **Step 1: Расширить types.Result**

```go
package types

type Result struct {
	GameURL     string
	ContentType string
	Image       []byte

	// Degraded marks a fallback card rendered from metadata because the
	// screenshot failed. Without this the pipeline reports success and a
	// broken NeonSportz layout goes unnoticed for weeks.
	Degraded bool
}
```

- [ ] **Step 2: Проставить признак в gochrome**

В `buildFallbackResult` добавить `Degraded: true`:

```go
	return types.Result{
		GameURL:     gameURL,
		ContentType: "image/png",
		Image:       imageBytes,
		Degraded:    true,
	}, nil
```

В обоих местах, где вызывается `buildFallbackResult`, повысить уровень лога до ERROR:

```go
		if isRecoverablePrecheckError(err) {
			log.Error().Err(err).Str("game_id", gameID).
				Msg("recap precheck degraded, falling back to metadata card")
			return buildFallbackResult(metaURL, gameURL, league, gameID)
		}
```

```go
	pngBytes, err := screenshotRecapWrapper(...)
	if err != nil {
		log.Error().Err(err).Str("game_id", gameID).
			Msg("screenshot failed, falling back to metadata card")
		return buildFallbackResult(metaURL, gameURL, league, gameID)
	}
```

- [ ] **Step 3: Записать признак в метаданные**

В `discord_kafka_game_image/internal/processor/processor.go` в `handleMessage`:

```go
		fetcherName := p.cfg.FetcherType
		if result.Degraded {
			fetcherName += "-fallback"
		}

		meta = &store.ImageMeta{
			ImageURL:    upload.URL,
			Bucket:      upload.Bucket,
			ObjectKey:   upload.ObjectKey,
			ContentType: upload.ContentType,
			Size:        upload.Size,
			Fetcher:     fetcherName,
			StoredAt:    time.Now(),
		}
```

И в формировании события заменить `Fetcher: p.cfg.FetcherType` на `Fetcher: meta.Fetcher`.

- [ ] **Step 4: Показать в дашборде**

В `internal/admin/store.go` в SQL-запросе `ListUnifiedMessages` добавить в SELECT:

```sql
				COALESCE(g.image->>'fetcher', gf.image->>'fetcher') AS image_fetcher,
```

Добавить в структуру `UnifiedMessage` поле `Fetcher string`, в сканирование — `var imageFetcher *string`, и присвоение `msg.Fetcher = normalizeValue(imageFetcher)`.

В `internal/admin/handlers_dashboard.go` добавить в `DashboardMessage` поле `Degraded bool` и заполнять его:

```go
			Degraded: strings.HasSuffix(message.Fetcher, "-fallback"),
```

В `cmd/admin_panel/templates/dashboard.html` в ячейку с картинкой добавить пометку:

```html
                <td>
                    {{if .ImageURL}}
                    <a href="{{.ImageURL}}" target="_blank" rel="noopener">
                        <img src="{{.ImageURL}}" alt="Game image" style="max-width: 56px; height: auto; display: block;">
                    </a>
                    {{if .Degraded}}<small style="color: #b45309;">fallback</small>{{end}}
                    {{else}}
                    <small style="color: #888;">-</small>
                    {{end}}
                </td>
```

- [ ] **Step 5: Проверить сборку**

Run: `go build ./... && go vet ./...`
Expected: успех.

- [ ] **Step 6: Commit**

```bash
git add discord_kafka_game_image/ internal/admin/ cmd/admin_panel/
git commit -m "feat: mark fallback cards as degraded

A failed screenshot produced a metadata card that recorded itself as a
normal gochrome result. The only way to tell them apart was file size,
so a NeonSportz redesign could break every screenshot while the
pipeline stayed green.

Fallbacks now log at error level and are labelled in the dashboard."
```

---

### Task V5-13: Мелкие фиксы и мёртвый код

**Зависит от:** V5-01. **Параллельно с:** V5-07…V5-12, V5-14…V5-17.
**Worktree:** `v5/13-cleanup`

**Files:**
- Modify: `discord_kafka_game_image/internal/middle/headless/headless.go:589-613`
- Create: `discord_kafka_game_image/internal/middle/headless/headless_test.go`
- Delete: `discord_kafka_game_image/internal/middle/selenium/selenium.go`
- Modify: `discord_kafka_game_image/internal/middle/middle.go` (убрать ветку selenium)
- Modify: `discord_kafka_listener/internal/kafka/buffered_writer.go` (убрать мёртвое поле)
- Modify: `docker-compose.yml` (убрать сервис selenium-chrome и том selenium-downloads)
- Modify: `.env.example` (убрать SELENIUM_*)
- **Не трогает:** gochrome, processor, store

**Контекст:** P1-5 (`parseColor` ломается на цветах > 0xFFFFFF), P3-1 (мёртвое поле `processing`), P3-2 (`retryItem.index` без `heap.Fix`), мёртвый пакет `selenium` с `log.Fatal()` без `.Msg()`.

- [ ] **Step 1: Написать падающий тест parseColor**

```go
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
```

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./discord_kafka_game_image/internal/middle/headless/ -run TestParseColor -v`
Expected: FAIL на `TestParseColorOverflow`.

- [ ] **Step 3: Починить parseColor**

```go
// parseColor converts NeonSportz's decimal colour into RGBA.
// Values outside the 24-bit range are clamped rather than sliced: the
// old code took the first two characters of a seven-digit hex string
// and produced an unrelated colour.
func parseColor(dec string) color.RGBA {
	value, err := strconv.ParseInt(strings.TrimSpace(dec), 10, 64)
	if err != nil || value < 0 {
		return color.RGBA{0, 0, 0, 255}
	}
	if value > 0xFFFFFF {
		value &= 0xFFFFFF
	}
	return color.RGBA{
		R: uint8((value >> 16) & 0xFF),
		G: uint8((value >> 8) & 0xFF),
		B: uint8(value & 0xFF),
		A: 255,
	}
}
```

- [ ] **Step 4: Удалить пакет selenium**

```bash
rm -r discord_kafka_game_image/internal/middle/selenium
```

В `middle.go` убрать импорт и ветку `case "selenium"`, оставив:

```go
func NewFetcher(ctx context.Context, cfg config.Config) (Fetcher, error) {
	switch cfg.FetcherType {
	case "gochrome":
		return gochrome.NewClient(ctx, cfg)
	case "headless":
		return headless.NewClient(ctx, cfg)
	default:
		return nil, &ErrUnsupportedFetcherType{FetcherType: cfg.FetcherType}
	}
}
```

В `config.go` game_image убрать `SeleniumURL`, `SeleniumDownloadDir` и валидацию `SELENIUM_URL`. В `docker-compose.yml` удалить сервис `selenium-chrome`, том `selenium-downloads` и монтирование этого тома у `discord-kafka-game-image`. В `.env.example` удалить `SELENIUM_URL` и `SELENIUM_DOWNLOAD_DIR`.

- [ ] **Step 5: Убрать мёртвое поле в buffered_writer**

В `discord_kafka_listener/internal/kafka/buffered_writer.go` удалить переменную `processing` и обе проверки с ней (строки 80, 118, 141, 145) — присваивание и чтение происходят в разных ветках одного `select`, поэтому при чтении из `w.incoming` значение всегда пустое. Удалить поле `index` из `retryItem` и его обновление в `Swap`/`Push` — `heap.Fix` нигде не вызывается.

- [ ] **Step 6: Запустить тесты и сборку**

Run: `go test ./... -short && go build ./... && go vet ./...`
Expected: успех.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "fix: clamp out-of-range colours, drop dead code

parseColor sliced the first two characters of a seven-digit hex string
and produced an unrelated colour for values above 0xFFFFFF.

Removes the selenium fetcher (profile disabled, and its unreachable
waitPage called log.Fatal() without .Msg(), which neither logs nor
exits), the buffered writer's processing field (set and cleared inside
one select branch, so never observable) and retryItem.index (heap.Fix
is never called)."
```

---

### Task V5-14: ListPending в pgstore

**Зависит от:** V5-06. **Блокирует:** V5-15.
**Worktree:** `v5/14-pgstore-listpending`

**Files:**
- Modify: `discord_kafka_game_image/internal/pgstore/store.go`
- Create: `discord_kafka_game_image/internal/pgstore/store_test.go`
- **Не трогает:** processor, mongo store

**Interfaces:**
- Produces: `func (s *Store) ListPending(ctx context.Context, limit int, retryInterval time.Duration) ([]Message, error)`

**Контекст:** метод существует только в Mongo-сторе, а `reprocessPending` использует именно его для добора недоделанной работы при старте. Без этого Mongo не удалить. Спека §4.3.

- [ ] **Step 1: Написать падающий тест**

```go
package pgstore

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/colzphml/mega_games/internal/common/pgtest"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	pool := pgtest.NewPostgres(t)
	s := &Store{pool: pool, log: zerolog.Nop()}
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return s
}

func TestListPendingReturnsNewMessages(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.EnsureMessage(ctx, "m1", []byte(`{"game_id":"1"}`)); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	got, err := s.ListPending(ctx, 100, time.Minute)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(got) != 1 || got[0].ID != "m1" {
		t.Fatalf("got %#v, want one message m1", got)
	}
}

func TestListPendingSkipsProcessed(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.EnsureMessage(ctx, "m2", []byte(`{"game_id":"2"}`)); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := s.MarkProcessed(ctx, "m2"); err != nil {
		t.Fatalf("mark processed: %v", err)
	}

	got, err := s.ListPending(ctx, 100, time.Minute)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d, want 0: processed messages must not be reprocessed", len(got))
	}
}

func TestListPendingReclaimsStuckInProgress(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.EnsureMessage(ctx, "m3", []byte(`{"game_id":"3"}`)); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := s.TouchAttempt(ctx, "m3", time.Minute); err != nil {
		t.Fatalf("touch: %v", err)
	}

	// Свежий in_progress не должен подхватываться.
	got, err := s.ListPending(ctx, 100, time.Minute)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d, want 0: an in-flight message must not be reclaimed", len(got))
	}

	// Застрявший — должен.
	if _, err := s.pool.Exec(ctx,
		`UPDATE game_image_status SET last_attempt_at = NOW() - INTERVAL '10 minutes' WHERE message_id = $1`,
		"m3"); err != nil {
		t.Fatalf("age the row: %v", err)
	}
	got, err = s.ListPending(ctx, 100, time.Minute)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d, want 1: a message stuck in progress must be reclaimed", len(got))
	}
}
```

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./discord_kafka_game_image/internal/pgstore/ -v`
Expected: FAIL — `undefined: ListPending`

- [ ] **Step 3: Реализовать**

```go
// ListPending returns messages that still need work: fresh ones, and
// ones abandoned mid-flight by a crashed worker. It replaces the Mongo
// implementation that reprocessPending relied on.
func (s *Store) ListPending(ctx context.Context, limit int, retryInterval time.Duration) ([]Message, error) {
	if limit <= 0 {
		limit = 1000
	}

	var rows pgx.Rows
	var err error
	if retryInterval > 0 {
		cutoff := queue.StaleCutoff(time.Now(), retryInterval)
		rows, err = s.pool.Query(ctx,
			`SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload, image
			 FROM game_image_status
			 WHERE (status = $1 AND (last_attempt_at IS NULL OR last_attempt_at < $3))
			    OR (status = $2 AND last_attempt_at < $3)
			 ORDER BY created_at ASC LIMIT $4`,
			queue.StatusNew, queue.StatusInProgress, cutoff, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT message_id, status, attempts, created_at, COALESCE(last_error, ''), last_attempt_at, payload, image
			 FROM game_image_status
			 WHERE status = $1 ORDER BY created_at ASC LIMIT $2`,
			queue.StatusNew, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.Status, &msg.Attempts, &msg.CreatedAt,
			&msg.LastError, &msg.LastAttempt, &msg.Payload, &msg.Image); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}
```

Добавить импорты `"github.com/jackc/pgx/v5"` и `"github.com/colzphml/mega_games/internal/common/queue"`.

- [ ] **Step 4: Запустить тесты**

Run: `go test ./discord_kafka_game_image/internal/pgstore/ -v`
Expected: PASS, все три теста.

- [ ] **Step 5: Commit**

```bash
git add discord_kafka_game_image/internal/pgstore/
git commit -m "feat: add ListPending to pgstore

reprocessPending recovers unfinished work at startup and only Mongo
could answer that query. Postgres needs the same capability before
Mongo can be removed."
```

---

### Task V5-15: Удаление MongoDB

**Зависит от:** V5-14. **Параллельно с:** ничем (трогает processor, который читает V5-12).

> **Порядок мержа:** V5-12 мержится раньше V5-15, так как обе задачи правят `processor.go`. При конфликте — приоритет у изменений V5-12 в `handleMessage`, у V5-15 в вызовах store.

**Worktree:** `v5/15-drop-mongo`

**Files:**
- Delete: `discord_kafka_game_image/internal/store/store.go`
- Modify: `discord_kafka_game_image/internal/processor/processor.go`
- Modify: `discord_kafka_game_image/cmd/discord-kafka-game-image/main.go`
- Modify: `discord_kafka_game_image/internal/config/config.go` (убрать MONGO_*)
- Modify: `docker-compose.yml` (убрать сервис mongo, оставить том)
- Modify: `.env.example` (убрать MONGO_*)
- Modify: `go.mod` (убрать mongo-driver)

**Контекст:** Mongo полностью дублирует `game_image_status`. Сверено на живом хосте: 600 документов в Mongo, 600 строк в Postgres, у всех заполнен `image_url`. Данные не мигрируют. Спека §2, §6.3 аудита.

- [ ] **Step 1: Перенести типы из Mongo-стора**

Создать `discord_kafka_game_image/internal/pgstore/types.go`:

```go
package pgstore

import "time"

type GamePayload struct {
	GameID  string `json:"game_id"`
	GameURL string `json:"game_url"`
}

type ImageMeta struct {
	ImageURL    string    `json:"image_url"`
	Bucket      string    `json:"image_bucket"`
	ObjectKey   string    `json:"image_object"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Fetcher     string    `json:"fetcher"`
	StoredAt    time.Time `json:"stored_at"`
}
```

- [ ] **Step 2: Переписать processor на один store**

Убрать поле `store *store.Store` из структуры `Processor`, оставить только `pg *pgstore.Store`. Упростить обёртки:

```go
func (p *Processor) touchAttempt(ctx context.Context, messageID string) error {
	return p.pg.TouchAttempt(ctx, messageID, p.cfg.ProcessRetryInterval)
}

func (p *Processor) recordAttempt(ctx context.Context, messageID, errMsg string) (pgstore.Message, error) {
	return p.pg.RecordAttempt(ctx, messageID, errMsg)
}

func (p *Processor) markProcessed(ctx context.Context, messageID string) error {
	return p.pg.MarkProcessed(ctx, messageID)
}

func (p *Processor) moveToFailed(ctx context.Context, messageID string, details map[string]any) error {
	return p.pg.MoveToFailed(ctx, messageID, details)
}
```

В `reprocessPending` и `consumeLoop` заменить `p.store.ListPending` на `p.pg.ListPending`, `store.GameMessage` на `pgstore.Message`, `store.StatusProcessed()` на `pgstore.StatusProcessed()`. Payload и image теперь хранятся как `[]byte` — распаковывать через `json.Unmarshal` там, где нужны поля.

- [ ] **Step 3: Убрать Mongo из main.go**

Удалить создание `storeClient`, `waitForMongo`, проверку mongo в health, импорт пакета `store`.

- [ ] **Step 4: Убрать Mongo из compose и env**

В `docker-compose.yml` удалить сервис `mongo` и его `depends_on` у `discord-kafka-game-image`. **Том `mongo-data` в секции `volumes:` оставить** — это страховка на случай отката.

В `.env.example` удалить блок `MONGO_*`.

- [ ] **Step 5: Проверить сборку и тесты**

```bash
go mod tidy
go build ./... && go vet ./... && go test ./... -short
```
Expected: успех, `go.mod` больше не содержит `mongo-driver`.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: drop MongoDB

Mongo stored exactly what game_image_status stores: status, attempts,
last error, payload and image metadata. The telegram sender that
actually consumes images never depended on it, and the admin panel
reads only Postgres. Verified on the live host: 600 documents against
600 rows, image_url populated in both.

The mongo-data volume is kept so a rollback loses nothing."
```

---

### Task V5-16: Оптимизация gochrome

**Зависит от:** V5-01. **Параллельно с:** V5-07…V5-14, V5-17.

> **Порядок мержа:** после V5-12 (обе правят `gochrome.go`).

**Worktree:** `v5/16-chrome-reuse`

**Files:**
- Modify: `discord_kafka_game_image/internal/middle/gochrome/gochrome.go:76-143, 273-282, 380-411`
- Modify: `discord_kafka_game_image/internal/middle/headless/headless.go:605-613`

**Interfaces:**
- Produces: `Client` получает поля `allocCtx context.Context`, `allocCancel context.CancelFunc`, `renders int`, `mu sync.Mutex`

**Контекст:** каждый `Fetch` создаёт новый `ExecAllocator`, то есть полный запуск процесса Chromium. При пачке из 12 игр это 12 холодных стартов по 2–3 с и 12 пиков памяти по 150–250 МБ. Шрифты парсятся ~15 раз на картинку. Спека §4.4.

- [ ] **Step 1: Кэшировать шрифты**

В `headless.go`:

```go
var (
	fontOnce   sync.Once
	parsedFont *truetype.Font
)

// loadFont reuses the parsed TTF. Parsing goregular.TTF on every call
// meant about fifteen parses per generated image, which is not free on
// an ARM board.
func loadFont(size float64) font.Face {
	fontOnce.Do(func() {
		parsedFont, _ = truetype.Parse(goregular.TTF)
	})
	if parsedFont == nil {
		return basicfont.Face7x13
	}
	return truetype.NewFace(parsedFont, &truetype.Options{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}
```

Добавить импорты `"sync"` и `"golang.org/x/image/font/basicfont"`.

Аналогично в `gochrome.go` для `scoreFont`.

- [ ] **Step 2: Переиспользовать браузер**

В `gochrome.go` заменить структуру и конструктор:

```go
type Client struct {
	baseURL       string
	league        string
	headless      bool
	screenshotDir string

	mu          sync.Mutex
	allocCtx    context.Context
	allocCancel context.CancelFunc
	renders     int
}

// maxRendersPerBrowser bounds how long one Chromium process lives.
// A weekly batch is about twelve games, so in normal operation the
// browser is never recycled mid-batch; the limit only guards against
// leaks accumulating over unusually long runs.
const maxRendersPerBrowser = 20

func NewClient(ctx context.Context, cfg config.Config) (*Client, error) {
	return &Client{
		baseURL:  strings.TrimRight(cfg.BaseURL, "/"),
		league:   strings.TrimSpace(cfg.League),
		headless: cfg.GoChromeHeadless,
	}, nil
}

// allocator returns a shared browser allocator, starting or recycling
// it as needed. Callers must not hold the lock while rendering.
func (c *Client) allocator() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.allocCtx != nil && c.renders < maxRendersPerBrowser {
		c.renders++
		return c.allocCtx
	}

	if c.allocCancel != nil {
		c.allocCancel()
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("headless", c.headless),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)
	c.allocCtx, c.allocCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	c.renders = 1
	return c.allocCtx
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.allocCancel != nil {
		c.allocCancel()
		c.allocCancel = nil
		c.allocCtx = nil
	}
	log.Info().Msg("closing gochrome client")
	return nil
}
```

- [ ] **Step 3: Использовать общий allocator в screenshotRecapWrapper**

Изменить сигнатуру, чтобы функция принимала allocator вместо создания своего:

```go
func screenshotRecapWrapper(
	allocCtx context.Context,
	gameURL string,
	overallTimeout time.Duration,
	sleepAfter time.Duration,
	assetsWait time.Duration,
	viewportW, viewportH int,
) ([]byte, error) {
	ctx, cancelTimeout := context.WithTimeout(allocCtx, overallTimeout)
	defer cancelTimeout()

	ctx, cancelTab := chromedp.NewContext(ctx)
	defer cancelTab()
	// ... остальное тело без изменений
```

В `Fetch` заменить вызов:

```go
	pngBytes, err := screenshotRecapWrapper(c.allocator(), gameURL, defaultTimeout,
		defaultSleepAfter, defaultAssetsWait, defaultViewportW, defaultViewportH)
```

- [ ] **Step 4: Проверить сборку**

Run: `go build ./... && go vet ./...`
Expected: успех.

- [ ] **Step 5: Проверить на живой игре**

```bash
docker compose build discord-kafka-game-image
docker compose up -d discord-kafka-game-image
docker compose logs -f discord-kafka-game-image
```
Дождаться обработки хотя бы двух игр, убедиться что вторая не запускает новый процесс Chromium (в логах нет повторного старта аллокатора) и что картинки нормального размера (~1,4 МБ, не ~100 КБ).

- [ ] **Step 6: Commit**

```bash
git add discord_kafka_game_image/internal/middle/
git commit -m "perf: reuse the browser across games, cache the parsed font

Every Fetch started a fresh Chromium process: twelve cold starts per
weekly batch, each with its own 150-250 MB peak. One allocator now
serves the batch and is recycled every twenty renders.

goregular.TTF was parsed about fifteen times per generated image."
```

---

### Task V5-17: Окно запроса дашборда

**Зависит от:** V5-01. **Параллельно с:** V5-07…V5-16.

> **Порядок мержа:** после V5-12 (обе правят `internal/admin/store.go`).

**Worktree:** `v5/17-dashboard-window`

**Files:**
- Modify: `internal/admin/store.go:192-254`
- Modify: `internal/admin/handlers_dashboard.go:73-79`

**Контекст:** P2-1. Полный скан шести таблиц с `UNION ALL` и `GROUP BY`, `LIMIT` применяется в самом конце. Отсюда 4,5% CPU у простаивающей админки. История сохраняется — ограничивается только отображение.

- [ ] **Step 1: Ограничить окно в CTE**

В `ListUnifiedMessages` изменить сигнатуру и первый CTE:

```go
// ListUnifiedMessages returns recent pipeline activity.
//
// The window matters: the CTE unions six status tables and groups the
// result, so without a date bound every dashboard render scanned every
// row ever written. History is untouched — only the view is bounded.
func (s *Store) ListUnifiedMessages(ctx context.Context, limit int, window time.Duration) ([]UnifiedMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	if window <= 0 {
		window = 30 * 24 * time.Hour
	}
	cutoff := time.Now().Add(-window)

	rows, err := s.pool.Query(ctx, `
		WITH all_messages AS (
			SELECT message_id, created_at FROM discord_message_status WHERE created_at > $2
			UNION ALL
			SELECT message_id, created_at FROM week_message_status WHERE created_at > $2
			UNION ALL
			SELECT message_id, created_at FROM game_image_status WHERE created_at > $2
			UNION ALL
			SELECT message_id, COALESCE(last_attempt_at, failed_at, first_seen_at) AS created_at
			  FROM game_image_failed
			 WHERE COALESCE(last_attempt_at, failed_at, first_seen_at) > $2
			UNION ALL
			SELECT message_id, created_at FROM telegram_week_status WHERE created_at > $2
			UNION ALL
			SELECT message_id, created_at FROM telegram_game_status WHERE created_at > $2
		),
		base AS (
			SELECT message_id, MAX(created_at) AS created_at
			FROM all_messages
			GROUP BY message_id
			ORDER BY created_at DESC
			LIMIT $1
		)
		SELECT
` + unifiedSelectTail, limit, cutoff)
```

Остальная часть запроса (от `base.message_id,` до конца) выносится в константу `unifiedSelectTail` без изменений, но с удалением финального `ORDER BY base.created_at DESC LIMIT $1` — сортировка и лимит переехали в CTE `base`, чтобы шесть JOIN выполнялись уже над 50 строками, а не над всей историей.

- [ ] **Step 2: Обновить вызов**

В `handlers_dashboard.go`:

```go
	messages, err := h.store.ListUnifiedMessages(r.Context(), 50, 30*24*time.Hour)
```

Добавить импорт `"time"`.

- [ ] **Step 3: Проверить сборку и работу**

Run: `go build ./... && go vet ./...`

```bash
docker compose up -d --build admin-panel
curl -s -o /dev/null -w "%{http_code} %{time_total}s\n" http://localhost:8002/
```
Expected: `200`, время ответа заметно меньше прежнего.

- [ ] **Step 4: Commit**

```bash
git add internal/admin/
git commit -m "perf: bound the dashboard query to a 30-day window

The CTE unioned six status tables and grouped the lot before applying
LIMIT, so every render scanned the full history. That is where the
4.5% CPU on an idle admin panel came from.

History is untouched; only the view is bounded."
```

---

### Task V5-31: Устойчивость headless к недоступным ассетам

**Зависит от:** V5-01. **Параллельно с:** V5-07…V5-17.

> **Порядок мержа:** после V5-13 (обе правят `headless.go`).

**Worktree:** `v5/31-headless-resilience`

**Files:**
- Modify: `discord_kafka_game_image/internal/middle/headless/headless.go:211-259, 286-319`
- Modify: `discord_kafka_game_image/internal/middle/headless/headless_test.go`

**Контекст:** P1-6. `buildRecapImage` последовательно грузит фон стадиона, два логотипа и логотип футера; любая ошибка возвращается наверх, и вся генерация проваливается. В `gochrome` fallback предусмотрен, в `headless` — нет. Поскольку `headless` остаётся запасным вариантом (спека §2, решение 6), он должен переживать 404 на логотипе.

- [ ] **Step 1: Написать падающий тест**

```go
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

	img, err := buildRecapImage(context.Background(), srv.Client(), srv.URL, 5*time.Second, rec)
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
	if _, err := buildRecapImage(context.Background(), srv.Client(), srv.URL, 5*time.Second, rec); err == nil {
		t.Error("without a background there is nothing to draw on; this must fail")
	}
}
```

Импорты теста: `"image"`, `"image/png"`, `"net/http"`, `"net/http/httptest"`, `"strings"`, `"time"`, `"context"`.

- [ ] **Step 2: Запустить тест, убедиться что падает**

Run: `go test ./discord_kafka_game_image/internal/middle/headless/ -run TestBuildRecapImage -v`
Expected: FAIL — 404 на логотипе роняет всю генерацию.

- [ ] **Step 3: Сделать логотипы необязательными**

```go
// optionalImage fetches a decorative asset. A missing team logo should
// degrade the card, not sink it: the scoreline is the point, the logo
// is decoration.
func optionalImage(ctx context.Context, doer ports.HTTPDoer, url string, timeout time.Duration) image.Image {
	img, err := loadImage(ctx, doer, url, timeout)
	if err != nil {
		log.Warn().Err(err).Str("url", url).Msg("optional asset unavailable, drawing without it")
		return nil
	}
	return img
}

func buildRecapImage(ctx context.Context, doer ports.HTTPDoer, baseURL string, httpTimeout time.Duration, recData Recap) (image.Image, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	// The stadium is the canvas; without it there is nothing to draw on.
	bg, err := loadImage(ctx, doer, baseURL+"/images/stadiums/"+strconv.Itoa(recData.Game.HomeTeam.LogoID)+".png", httpTimeout)
	if err != nil {
		return nil, fmt.Errorf("load stadium background: %w", err)
	}

	logoHome := optionalLogo(ctx, doer, baseURL, recData.Game.HomeTeam, httpTimeout)
	logoAway := optionalLogo(ctx, doer, baseURL, recData.Game.AwayTeam, httpTimeout)
	footerLogo := optionalImage(ctx, doer, baseURL+"/logo.png", httpTimeout)

	const width, height = 2048, 1152
	dc := gg.NewContext(width, height)
	dc.DrawImage(bg, 0, 0)
	drawSideGradient(dc, width, height)

	cx, cy := 256.0, 64.0
	cw, ch := 1536.0, 864.0
	drawContainer(dc, cx, cy, cw, ch)
	drawTopBar(dc, recData, cx, cy, cw)
	drawTeamRows(dc, recData, cx, cy+128, cw, logoAway, logoHome)
	drawStatsSection(dc, recData, cx, cy+128+2*152, cw)
	drawFooter(dc, cx, cy+864-68, cw, footerLogo)

	return dc.Image(), nil
}

func optionalLogo(ctx context.Context, doer ports.HTTPDoer, baseURL string, t Team, timeout time.Duration) image.Image {
	if t.Logo != nil && *t.Logo != "" {
		logoURL := *t.Logo
		if !strings.HasPrefix(logoURL, "http://") && !strings.HasPrefix(logoURL, "https://") {
			logoURL = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(logoURL, "/")
		}
		return optionalImage(ctx, doer, logoURL, timeout)
	}
	return optionalImage(ctx, doer, strings.TrimRight(baseURL, "/")+"/images/teamlogos/256/"+strconv.Itoa(t.LogoID)+".png", timeout)
}
```

- [ ] **Step 4: Защитить отрисовку от nil**

В `drawTeamRow` обернуть отрисовку логотипа:

```go
	if logo != nil {
		dc.DrawImageAnchored(logo, int(x+colLogoW/2), int(y+h/2), 0.5, 0.5)
	}
```

`drawFooter` уже содержит проверку `if logo != nil`.

- [ ] **Step 5: Запустить тесты**

Run: `go test ./discord_kafka_game_image/internal/middle/headless/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add discord_kafka_game_image/internal/middle/headless/
git commit -m "fix: keep rendering when a team logo is unavailable

A 404 on any single asset failed the entire card. The stadium is the
canvas and stays required; logos are decoration and now degrade
gracefully.

headless is the documented fallback renderer, so it has to survive the
conditions that make the primary one fail."
```

---

Продолжение — фазы 3–5 — в файле `2026-07-28-mega-games-v5-part3.md`.
