package admin

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"

	"github.com/rs/zerolog/log"
)

type TeamsHandler struct {
	store *Store
	tmpl  *template.Template
}

func NewTeamsHandler(store *Store) (*TeamsHandler, error) {
	tmpl, err := template.ParseFiles(
		"cmd/admin_panel/templates/layout.html",
		"cmd/admin_panel/templates/teams.html",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &TeamsHandler{
		store: store,
		tmpl:  tmpl,
	}, nil
}

func (h *TeamsHandler) List(w http.ResponseWriter, r *http.Request) {
	teams, err := h.store.ListTeams(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("failed to list teams")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := h.tmpl.ExecuteTemplate(w, "layout.html", teams); err != nil {
		log.Error().Err(err).Msg("failed to render template")
	}
}

func (h *TeamsHandler) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	team := Team{
		Name:      r.FormValue("name"),
		ShortName: r.FormValue("short_name"),
		Emoji:     r.FormValue("emoji"),
		Player:    r.FormValue("player"),
	}

	if err := h.store.CreateTeam(r.Context(), team); err != nil {
		if errors.Is(err, ErrConflict) {
			http.Error(w, "Team already exists", http.StatusConflict)
			return
		}
		log.Error().Err(err).Msg("failed to create team")
		http.Error(w, "Failed to create team", http.StatusInternalServerError)
		return
	}

	if err := h.tmpl.ExecuteTemplate(w, "team-row", team); err != nil {
		log.Error().Err(err).Msg("failed to render template")
	}
}

func (h *TeamsHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	team, err := h.store.GetTeam(r.Context(), name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "Team not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Msg("failed to get team")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := h.tmpl.ExecuteTemplate(w, "team-row", team); err != nil {
		log.Error().Err(err).Msg("failed to render template")
	}
}

func (h *TeamsHandler) EditForm(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	team, err := h.store.GetTeam(r.Context(), name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "Team not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Msg("failed to get team")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := h.tmpl.ExecuteTemplate(w, "team-edit-row", team); err != nil {
		log.Error().Err(err).Msg("failed to render template")
	}
}

func (h *TeamsHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	team := Team{
		Name:      name,
		ShortName: r.FormValue("short_name"),
		Emoji:     r.FormValue("emoji"),
		Player:    r.FormValue("player"),
	}

	if err := h.store.UpdateTeam(r.Context(), team); err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "Team not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Msg("failed to update team")
		http.Error(w, "Failed to update team", http.StatusInternalServerError)
		return
	}

	if err := h.tmpl.ExecuteTemplate(w, "team-row", team); err != nil {
		log.Error().Err(err).Msg("failed to render template")
	}
}

func (h *TeamsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.store.DeleteTeam(r.Context(), name); err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "Team not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Msg("failed to delete team")
		http.Error(w, "Failed to delete team", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
