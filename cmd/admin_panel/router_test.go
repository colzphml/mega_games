package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/colzphml/mega_games/internal/admin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// chdirToRepoRoot points the relative template/asset paths that
// internal/admin's handler constructors use ("cmd/admin_panel/...") at the
// repo root, matching how the compiled binary is always run in production:
// Dockerfile.admin sets WORKDIR /app and copies the templates to
// /app/cmd/admin_panel/templates. go test instead starts a package's test
// binary from that package's own source directory, so without this,
// newRouter below fails to parse its templates and every test in this file
// would fail regardless of the auth wiring being tested.
func chdirToRepoRoot(t *testing.T) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir to repo root %q: %v", root, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

// testRouter builds the router exactly as main does, minus the parts of
// main that need a reachable Postgres: pgxpool.New only parses the DSN and
// dials lazily in the background, so no live database is required to
// exercise routing and auth. Handlers that do need the database (e.g. a
// successfully-authenticated GET /teams) will fail at query time, which is
// fine -- these tests only assert on the auth boundary, not on full
// request success.
func testRouter(t *testing.T, adminPassword string) http.Handler {
	t.Helper()
	chdirToRepoRoot(t)

	pool, err := pgxpool.New(context.Background(), "postgres://test:test@127.0.0.1:1/testdb?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	store := admin.NewStore(pool)
	cfg := admin.Config{AdminUser: "admin", AdminPassword: adminPassword}
	router, err := newRouter(cfg, store)
	if err != nil {
		t.Fatalf("newRouter: %v", err)
	}
	return router
}

// TestHealthIsReachableWithoutCredentials guards the flip side of the auth
// wiring: the container healthcheck carries no credentials, so /health
// must stay outside BasicAuth, or a correctly-configured admin panel would
// be marked unhealthy and endlessly autoheal-restarted.
func TestHealthIsReachableWithoutCredentials(t *testing.T) {
	router := testRouter(t, "secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health = %d, want 200: the container healthcheck has no "+
			"credentials and must not be blocked by auth", rec.Code)
	}
}

// TestAdminRoutesRejectMissingCredentials is the regression test for the
// most dangerous gap found in this codebase: cmd/admin_panel had no tests
// at all, so nothing verified that BasicAuth was actually wired onto the
// routes it guards, as opposed to just being correct in isolation (which
// internal/admin/auth_test.go already covers well). Dropping the guard in
// newRouter -- e.g. handing root.Handle the bare mux instead of guarded --
// is a one-line change that leaves DELETE /teams/{name} and
// POST /schedule/upload reachable by anyone on the network, with every
// other test in the repo still green.
func TestAdminRoutesRejectMissingCredentials(t *testing.T) {
	router := testRouter(t, "secret")

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/"},
		{http.MethodGet, "/teams"},
		{http.MethodDelete, "/teams/Falcons"},
		{http.MethodPost, "/schedule/upload"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s without credentials = %d, want 401: this route "+
					"must sit behind BasicAuth, or the admin panel -- including "+
					"team deletion and schedule uploads -- is reachable to "+
					"anyone on the network", tc.method, tc.path, rec.Code)
			}
		})
	}
}

// TestAdminRoutesAcceptCorrectCredentials proves the guard isn't so broad
// it locks out a correctly-authenticated request too. GET /schedule is
// used because it renders straight from a template with no database
// query, so this stays independent of whether Postgres is reachable.
func TestAdminRoutesAcceptCorrectCredentials(t *testing.T) {
	router := testRouter(t, "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/schedule", nil)
	req.SetBasicAuth("admin", "secret")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /schedule with correct credentials = %d, want 200: the "+
			"guard must not reject a valid admin/secret login", rec.Code)
	}
}
