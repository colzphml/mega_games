package admin

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/rs/zerolog/log"
)

type DashboardHandler struct {
	store         *Store
	dashboardTmpl *template.Template
	logsTmpl      *template.Template
	scheduleTmpl  *template.Template
}

type DashboardMessage struct {
	MessageID           string
	CreatedAt           string
	ProcessorStatus     string
	WeekFormatterStatus string
	GameImageStatus     string
	TelegramWeekStatus  string
	TelegramGameStatus  string
}

type DashboardViewData struct {
	Messages []DashboardMessage
}

func NewDashboardHandler(store *Store) (*DashboardHandler, error) {
	dashboardTmpl, err := template.ParseFiles(
		"cmd/admin_panel/templates/layout.html",
		"cmd/admin_panel/templates/dashboard.html",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dashboard templates: %w", err)
	}

	logsTmpl, err := template.ParseFiles(
		"cmd/admin_panel/templates/layout.html",
		"cmd/admin_panel/templates/logs.html",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse logs templates: %w", err)
	}

	scheduleTmpl, err := template.ParseFiles(
		"cmd/admin_panel/templates/layout.html",
		"cmd/admin_panel/templates/schedule.html",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schedule templates: %w", err)
	}

	return &DashboardHandler{
		store:         store,
		dashboardTmpl: dashboardTmpl,
		logsTmpl:      logsTmpl,
		scheduleTmpl:  scheduleTmpl,
	}, nil
}

func (h *DashboardHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	messages, err := h.store.ListUnifiedMessages(r.Context(), 50)
	if err != nil {
		log.Error().Err(err).Msg("failed to get unified messages")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	viewData := DashboardViewData{
		Messages: make([]DashboardMessage, 0, len(messages)),
	}
	for _, message := range messages {
		viewData.Messages = append(viewData.Messages, DashboardMessage{
			MessageID:           message.MessageID,
			CreatedAt:           message.CreatedAt.Format("2006-01-02 15:04:05"),
			ProcessorStatus:     message.ProcessorStatus,
			WeekFormatterStatus: message.WeekFormatterStatus,
			GameImageStatus:     message.GameImageStatus,
			TelegramWeekStatus:  message.TelegramWeekStatus,
			TelegramGameStatus:  message.TelegramGameStatus,
		})
	}

	if err := h.dashboardTmpl.ExecuteTemplate(w, "layout.html", viewData); err != nil {
		log.Error().Err(err).Msg("failed to render dashboard template")
	}
}

func (h *DashboardHandler) Logs(w http.ResponseWriter, r *http.Request) {
	if err := h.logsTmpl.ExecuteTemplate(w, "layout.html", nil); err != nil {
		log.Error().Err(err).Msg("failed to render logs template")
	}
}

func (h *DashboardHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	if err := h.scheduleTmpl.ExecuteTemplate(w, "layout.html", nil); err != nil {
		log.Error().Err(err).Msg("failed to render schedule template")
	}
}
