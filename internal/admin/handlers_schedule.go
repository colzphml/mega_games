package admin

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

type ScheduleHandler struct {
	store scheduleStore
}

type scheduleStore interface {
	UpsertGame(context.Context, Game) error
}

func NewScheduleHandler(store scheduleStore) *ScheduleHandler {
	return &ScheduleHandler{
		store: store,
	}
}

func (h *ScheduleHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		log.Error().Err(err).Msg("failed to parse multipart form")
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("MEGA_games.csv")
	if err != nil {
		log.Error().Err(err).Msg("failed to get file MEGA_games.csv")
		http.Error(w, "Missing file MEGA_games.csv", http.StatusBadRequest)
		return
	}
	defer file.Close()

	games, err := readScheduleCSV(file)
	if err != nil {
		log.Error().Err(err).Msg("invalid schedule CSV")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	for _, game := range games {
		if err := h.store.UpsertGame(r.Context(), game); err != nil {
			log.Error().Err(err).Interface("game", game).Msg("failed to upsert game")
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, "<div class='p-4 bg-green-100 text-green-800 rounded'>Successfully uploaded %d games</div>", len(games))
}

func readScheduleCSV(input io.Reader) ([]Game, error) {
	reader := csv.NewReader(input)
	header, err := reader.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("CSV header: empty file")
	}
	if err != nil {
		return nil, fmt.Errorf("CSV header: %w", err)
	}
	columns := make(map[string]int, len(header))
	for i, name := range header {
		name = strings.TrimSpace(name)
		if _, exists := columns[name]; exists {
			return nil, fmt.Errorf("CSV header: duplicate column %q", name)
		}
		columns[name] = i
	}
	for _, name := range []string{"homeTeam", "awayTeam", "seasonIndex", "stageIndex", "weekIndex"} {
		if _, exists := columns[name]; !exists {
			return nil, fmt.Errorf("CSV header: missing required column %q", name)
		}
	}

	var games []Game
	for row := 2; ; row++ {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV row %d: %w", row, err)
		}
		for _, name := range []string{"homeTeam", "awayTeam"} {
			if strings.TrimSpace(record[columns[name]]) == "" {
				return nil, fmt.Errorf("CSV row %d: missing %s", row, name)
			}
		}
		readIndex := func(name string) (int, error) {
			index, err := strconv.Atoi(strings.TrimSpace(record[columns[name]]))
			if err != nil {
				return 0, fmt.Errorf("CSV row %d: invalid %s: %w", row, name, err)
			}
			if index < 0 {
				return 0, fmt.Errorf("CSV row %d: %s must be nonnegative", row, name)
			}
			return index, nil
		}
		seasonIndex, err := readIndex("seasonIndex")
		if err != nil {
			return nil, err
		}
		stageIndex, err := readIndex("stageIndex")
		if err != nil {
			return nil, err
		}
		weekIndex, err := readIndex("weekIndex")
		if err != nil {
			return nil, err
		}

		games = append(games, Game{
			SeasonIndex: seasonIndex,
			Stage:       stageIndex > 0,
			WeekIndex:   weekIndex,
			Home:        record[columns["homeTeam"]],
			Away:        record[columns["awayTeam"]],
		})
	}
	if len(games) == 0 {
		return nil, fmt.Errorf("CSV contains no games")
	}
	return games, nil
}
