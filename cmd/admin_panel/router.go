package main

import (
	"fmt"
	"net/http"

	"github.com/colzphml/mega_games/internal/admin"
)

// newRouter assembles the admin panel's HTTP routing exactly as main wires
// it in production: the admin UI and API behind BasicAuth, with /health
// left unguarded because the container healthcheck carries no credentials.
//
// It is factored out of main so a test can assemble the very same router
// -- including the auth wiring -- without going through main's
// os.Exit-on-error startup (env config, connecting to postgres, signal
// handling). cmd/admin_panel had no tests at all before this: removing the
// BasicAuth wrapping below was a one-line change that left the admin panel,
// including DELETE /teams/{name} and POST /schedule/upload, reachable by
// anyone on the network with every other test in the repo still green.
func newRouter(cfg admin.Config, store *admin.Store) (http.Handler, error) {
	mux := http.NewServeMux()

	teamsHandler, err := admin.NewTeamsHandler(store)
	if err != nil {
		return nil, fmt.Errorf("failed to create teams handler: %w", err)
	}

	dashboardHandler, err := admin.NewDashboardHandler(store)
	if err != nil {
		return nil, fmt.Errorf("failed to create dashboard handler: %w", err)
	}

	scheduleHandler := admin.NewScheduleHandler(store)

	mux.HandleFunc("GET /", dashboardHandler.Dashboard)
	mux.HandleFunc("GET /logs", dashboardHandler.Logs)
	mux.HandleFunc("GET /schedule", dashboardHandler.Schedule)

	mux.HandleFunc("GET /teams", teamsHandler.List)
	mux.HandleFunc("POST /teams", teamsHandler.Create)
	mux.HandleFunc("GET /teams/{name}", teamsHandler.Get)
	mux.HandleFunc("GET /teams/{name}/edit", teamsHandler.EditForm)
	mux.HandleFunc("PUT /teams/{name}", teamsHandler.Update)
	mux.HandleFunc("DELETE /teams/{name}", teamsHandler.Delete)

	mux.HandleFunc("POST /schedule/upload", scheduleHandler.Upload)

	// Serve static assets
	// Assumes running from project root
	fs := http.FileServer(http.Dir("cmd/admin_panel/assets"))
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", fs))

	// The admin UI and API sit behind basic auth: it exposes
	// DELETE /teams/{name} and POST /schedule/upload, which used to be
	// reachable by anyone on the network. /health stays outside the
	// guard because the container healthcheck has no credentials.
	guarded := admin.BasicAuth(cfg.AdminUser, cfg.AdminPassword, mux)

	root := http.NewServeMux()
	root.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	root.Handle("/", guarded)

	return root, nil
}
