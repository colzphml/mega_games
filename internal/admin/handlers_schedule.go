package admin

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/rs/zerolog/log"
)

type ScheduleHandler struct {
	store *Store
}

func NewScheduleHandler(store *Store) *ScheduleHandler {
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

	reader := csv.NewReader(file)
	if _, err := reader.Read(); err != nil {
		log.Error().Err(err).Msg("failed to read CSV header")
		http.Error(w, "Invalid CSV", http.StatusBadRequest)
		return
	}

	count := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Error().Err(err).Msg("failed to read CSV record")
			http.Error(w, "Error reading CSV", http.StatusInternalServerError)
			return
		}

		if len(record) <= 11 {
			continue
		}

		seasonIndex, err := strconv.Atoi(record[9])
		if err != nil {
			log.Debug().Err(err).Str("value", record[9]).Msg("skipping row due to invalid season index")
			continue
		}
		stageIndex, err := strconv.Atoi(record[10])
		if err != nil {
			log.Debug().Err(err).Str("value", record[10]).Msg("skipping row due to invalid stage index")
			continue
		}
		weekIndex, err := strconv.Atoi(record[11])
		if err != nil {
			log.Debug().Err(err).Str("value", record[11]).Msg("skipping row due to invalid week index")
			continue
		}

		game := Game{
			SeasonIndex: seasonIndex,
			Stage:       stageIndex > 0,
			WeekIndex:   weekIndex,
			Home:        record[3],
			Away:        record[4],
		}

		if err := h.store.UpsertGame(r.Context(), game); err != nil {
			log.Error().Err(err).Interface("game", game).Msg("failed to upsert game")
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		count++
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, "<div class='p-4 bg-green-100 text-green-800 rounded'>Successfully uploaded %d games</div>", count)
}
