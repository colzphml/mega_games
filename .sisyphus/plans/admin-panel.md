# Admin Panel Implementation Plan

## TL;DR

> **Quick Summary**: Create a local-only Admin Panel service (Go/HTML/HTMX) to manage `teams`, upload `schedule_games`, and view system logs/statuses.
>
> **Deliverables**:
> - New service `admin_panel` in `cmd/admin_panel`
> - UI for Dashboard, Teams, Schedule, Logs
> - Database migration (Unique constraints for Schedule)
> - Updated `docker-compose.yml`
>
> **Estimated Effort**: Medium
> **Parallel Execution**: YES - 2 waves
> **Critical Path**: DB Migration → Service Base → Feature Implementation

---

## Context

### Original Request
User wants a localhost Admin Panel to:
- View entity statuses (Dashboard)
- Manage Users (Teams)
- Upload Schedule (`MEGA_games.csv` -> DB update)
- View Logs (Grafana integration)

### Technical Decisions
- **Stack**: Go (stdlib `net/http`) + `html/template` + HTMX for interactivity.
- **DB Strategy**: Connect to existing `megagames` Postgres.
- **Schedule Logic**: Add UNIQUE constraint to `schedule_games` to enable UPSERT behavior.
- **Logs**: Embed existing Grafana (`localhost:3000`) via iframe.
- **Security**: No auth (Localhost only).

---

## Work Objectives

### Core Objective
Provide a unified web interface for managing the bot's data and monitoring its health.

### Concrete Deliverables
- `cmd/admin_panel/main.go`
- `internal/admin/...` (Handlers, Store, Templates)
- SQL Migration script for `schedule_games`
- Docker Compose entry for `admin-panel`

### Definition of Done
- [ ] `http://localhost:8081` opens the dashboard
- [ ] Can add/edit/delete a Team
- [ ] Can upload `MEGA_games.csv` and see `schedule_games` updated without duplicates
- [ ] Can see logs from Grafana in the Logs tab
- [ ] Can see status counts (New/Processed/Failed) for services

### Must Have
- Duplicate prevention for Schedule (Unique Constraint)
- HTMX-based partial updates (no full reloads for tables)
- Graceful error handling for CSV parsing

### Must NOT Have
- Complex Authentication
- External internet access requirements (assets should be local or CDN if acceptable, preferring embedded/local for "localhost" reliability)

---

## Verification Strategy

### Manual Verification (Localhost)
Since this is a GUI tool, verification will be primarily manual using `curl` for API endpoints and Browser for UI.

- **Frontend**: Agent will use `curl` to verify HTTP 200 OK and HTML content presence.
- **Backend Logic**: Agent will use `psql` to verify DB state changes (Insert/Update).
- **Logs**: Verify Grafana iframe URL is correct.

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Setup & Core):
├── Task 1: DB Migration (Deduplicate & Constraint)
├── Task 2: Service Scaffolding (Main, HTTP Server, Assets)
└── Task 3: Store Implementation (Basic shared queries)

Wave 2 (Features):
├── Task 4: Teams Management (CRUD)
├── Task 5: Schedule Upload (CSV Parsing + Upsert)
├── Task 6: Dashboard & Logs (Status aggregation + Grafana embed)
└── Task 7: Docker Compose Integration
```

---

## TODOs

- [x] 1. **Database Migration: Schedule Constraints**

  **What to do**:
  - Create a migration script `discord_tools/sql/migration_001_schedule_constraint.sql`.
  - Logic:
    1. Identify and remove duplicate rows from `schedule_games` (keep latest).
    2. Add `CONSTRAINT unique_game UNIQUE (season_index, stage, week_index, home_team, away_team)`.
  - Execute this script against the running DB.

  **Recommended Agent**:
  - **Category**: `quick`
  - **Skills**: [`bash`]

  **Verification**:
  - `psql ... -c "\d schedule_games"` shows the UNIQUE constraint.
  - Attempting to insert a duplicate manually fails.

- [x] 2. **Service Scaffolding: Admin Panel**

  **What to do**:
  - Create `cmd/admin_panel/main.go`.
  - Create `cmd/admin_panel/assets/` (css, js).
  - Create `cmd/admin_panel/templates/` (layout.html, index.html).
  - Setup `net/http` server on port 8081.
  - Setup Postgres connection pool (copy config pattern from other services).

  **Recommended Agent**:
  - **Category**: `visual-engineering`
  - **Skills**: [`frontend-ui-ux`]

  **Verification**:
  - `go run ./cmd/admin_panel/main.go` starts without error.
  - `curl localhost:8081` returns HTML.

- [x] 3. **Feature: Teams Management (CRUD)**

  **What to do**:
  - Implement `GET /teams` (List).
  - Implement `POST /teams` (Create).
  - Implement `PUT /teams/{name}` (Update).
  - Implement `DELETE /teams/{name}` (Delete).
  - Use HTMX for inline editing/table updates.

  **References**:
  - `discord_tools/cmd/csv-to-sql/main.go` (Team struct definition).

  **Recommended Agent**:
  - **Category**: `visual-engineering`
  - **Skills**: [`frontend-ui-ux`]

  **Verification**:
  - `curl -X POST ...` creates a team.
  - `psql` confirms new team exists.

- [x] 4. **Feature: Schedule Management (CSV Upload)**

  **What to do**:
  - Implement `POST /schedule/upload`.
  - Parse CSV (reuse logic from `discord_tools` or copy).
  - Perform `INSERT ... ON CONFLICT DO UPDATE` for each row.
  - UI: File input + Upload button + Success/Error message.

  **References**:
  - `discord_tools/cmd/csv-to-sql/main.go` (CSV parsing logic).

  **Recommended Agent**:
  - **Category**: `quick`
  - **Skills**: [`bash`]

  **Verification**:
  - Upload a CSV with 1 changed game.
  - Verify DB reflects the change.
  - Verify no duplicates created.

- [x] 5. **Feature: Dashboard & Logs**

  **What to do**:
  - **Dashboard**: Query counts of `status='new'`, `status='processed'`, `status='failed'` from `discord_message_status` (and other status tables if they exist - check `\dt` first).
  - **Logs**: Create a page with `<iframe src="http://localhost:3000/d/..." width="100%" height="800">`.
  - Add navigation menu.

  **Recommended Agent**:
  - **Category**: `visual-engineering`
  - **Skills**: [`frontend-ui-ux`]

  **Verification**:
  - Dashboard shows non-zero counts (if DB has data).
  - Logs page contains iframe.

- [x] 6. **Deployment: Docker Compose**

  **What to do**:
  - Add `admin-panel` service to `docker-compose.yml`.
  - Map port `8081:8081`.
  - Add `depends_on: postgres`.
  - Add environment variables (DB credentials).

  **Recommended Agent**:
  - **Category**: `quick`
  - **Skills**: [`bash`]

  **Verification**:
  - `docker compose up -d admin-panel` starts successfully.
  - `docker compose ps` shows it healthy/running.

---

## Success Criteria

### Final Checklist
- [ ] Admin Panel accessible at `http://localhost:8081`.
- [ ] Schedule upload is idempotent (run twice = same result).
- [ ] Logs are visible via Grafana embed.
- [ ] Teams can be modified via UI.
