package admin

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestScheduleUploadRejectsInvalidNewExport(t *testing.T) {
	data := "id,gameId,league,homeTeam,awayTeam,awayScore,homeScore,isGameOfTheWeek,manual_gotw,gotw_reason,seasonIndex,stageIndex,weekIndex,status\n1,42,MEGA,Bills,Jets,0,0,false,false,rivalry,0,1,bad,scheduled\n"
	response := httptest.NewRecorder()
	store := &scheduleMemoryStore{}
	NewScheduleHandler(store).Upload(response, scheduleUploadRequest(t, data))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %q", response.Code, response.Body.String())
	}
	for _, fragment := range []string{"row 2", "weekIndex"} {
		if !strings.Contains(response.Body.String(), fragment) {
			t.Errorf("body %q lacks context %q", response.Body.String(), fragment)
		}
	}
	if len(store.games) != 0 {
		t.Fatalf("invalid upload wrote games: %#v", store.games)
	}
}

func TestScheduleUploadUsesNamedColumns(t *testing.T) {
	want := []Game{{SeasonIndex: 0, Stage: true, WeekIndex: 7, Home: "Bills", Away: "Jets"}}
	for _, tc := range []struct {
		name string
		csv  string
	}{
		{"old export", "id,gameId,league,homeTeam,awayTeam,awayScore,homeScore,isGameOfTheWeek,manual_gotw,seasonIndex,stageIndex,weekIndex,status\n1,42,MEGA,Bills,Jets,0,0,false,false,0,1,7,scheduled\n"},
		{"new export with gotw_reason", "id,gameId,league,homeTeam,awayTeam,awayScore,homeScore,isGameOfTheWeek,manual_gotw,gotw_reason,seasonIndex,stageIndex,weekIndex,status\n1,42,MEGA,Bills,Jets,0,0,false,false,rivalry,0,1,7,scheduled\n"},
		{"reordered columns with extras", "weekIndex,gotw_reason,awayTeam,stageIndex,homeTeam,seasonIndex\n7,rivalry,Jets,1,Bills,0\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &scheduleMemoryStore{}
			response := httptest.NewRecorder()
			NewScheduleHandler(store).Upload(response, scheduleUploadRequest(t, tc.csv))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d; body = %q", response.Code, response.Body.String())
			}
			if !reflect.DeepEqual(store.games, want) {
				t.Fatalf("stored games = %#v, want %#v", store.games, want)
			}
			if !strings.Contains(response.Body.String(), "uploaded 1 games") {
				t.Errorf("body = %q, want upload count 1", response.Body.String())
			}
		})
	}
}

func TestScheduleUploadValidatesEntireFileBeforeWriting(t *testing.T) {
	header := "id,gameId,league,homeTeam,awayTeam,awayScore,homeScore,isGameOfTheWeek,manual_gotw,seasonIndex,stageIndex,weekIndex,status\n"
	valid := "1,42,MEGA,Bills,Jets,0,0,false,false,0,1,7,scheduled\n"
	for _, tc := range []struct {
		name    string
		csv     string
		context []string
	}{
		{"empty file", "", []string{"header"}},
		{"header only", header, []string{"no games"}},
		{"missing home header", "awayTeam,seasonIndex,stageIndex,weekIndex\nJets,0,1,7\n", []string{"header", "homeTeam"}},
		{"missing away header", "homeTeam,seasonIndex,stageIndex,weekIndex\nBills,0,1,7\n", []string{"header", "awayTeam"}},
		{"missing season header", "homeTeam,awayTeam,stageIndex,weekIndex\nBills,Jets,1,7\n", []string{"header", "seasonIndex"}},
		{"missing stage header", "homeTeam,awayTeam,seasonIndex,weekIndex\nBills,Jets,0,7\n", []string{"header", "stageIndex"}},
		{"missing header", "homeTeam,awayTeam,seasonIndex,stageIndex\nBills,Jets,0,1\n", []string{"header", "weekIndex"}},
		{"duplicate header", "homeTeam,awayTeam,seasonIndex,stageIndex,weekIndex,weekIndex\nBills,Jets,0,1,7,8\n", []string{"header", "weekIndex"}},
		{"invalid season", header + valid + "2,43,MEGA,Giants,Eagles,0,0,false,false,no,1,7,scheduled\n", []string{"row 3", "seasonIndex"}},
		{"missing season", header + valid + "2,43,MEGA,Giants,Eagles,0,0,false,false,,1,7,scheduled\n", []string{"row 3", "seasonIndex"}},
		{"negative season", header + valid + "2,43,MEGA,Giants,Eagles,0,0,false,false,-1,1,7,scheduled\n", []string{"row 3", "seasonIndex"}},
		{"invalid stage", header + valid + "2,43,MEGA,Giants,Eagles,0,0,false,false,0,regular,7,scheduled\n", []string{"row 3", "stageIndex"}},
		{"missing stage", header + valid + "2,43,MEGA,Giants,Eagles,0,0,false,false,0,,7,scheduled\n", []string{"row 3", "stageIndex"}},
		{"negative stage", header + valid + "2,43,MEGA,Giants,Eagles,0,0,false,false,0,-1,7,scheduled\n", []string{"row 3", "stageIndex"}},
		{"invalid week", header + valid + "2,43,MEGA,Giants,Eagles,0,0,false,false,0,1,bad,scheduled\n", []string{"row 3", "weekIndex"}},
		{"missing week", header + valid + "2,43,MEGA,Giants,Eagles,0,0,false,false,0,1,,scheduled\n", []string{"row 3", "weekIndex"}},
		{"negative week", header + valid + "2,43,MEGA,Giants,Eagles,0,0,false,false,0,1,-1,scheduled\n", []string{"row 3", "weekIndex"}},
		{"missing home team", header + valid + "2,43,MEGA,,Eagles,0,0,false,false,0,1,7,scheduled\n", []string{"row 3", "homeTeam"}},
		{"blank away team", header + valid + "2,43,MEGA,Giants,   ,0,0,false,false,0,1,7,scheduled\n", []string{"row 3", "awayTeam"}},
		{"short row", header + valid + "2,43,MEGA,Giants,Eagles,0,0,false,false,0,1\n", []string{"row 3"}},
		{"broken quoting", header + valid + "\"Giants,Eagles,0,1,7\n", []string{"row 3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &scheduleMemoryStore{}
			response := httptest.NewRecorder()
			NewScheduleHandler(store).Upload(response, scheduleUploadRequest(t, tc.csv))
			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body = %q", response.Code, response.Body.String())
			}
			if len(store.games) != 0 {
				t.Errorf("invalid upload wrote partial schedule: %#v", store.games)
			}
			for _, fragment := range tc.context {
				if !strings.Contains(response.Body.String(), fragment) {
					t.Errorf("body %q lacks context %q", response.Body.String(), fragment)
				}
			}
		})
	}
}

func TestScheduleUploadPreservesStageMapping(t *testing.T) {
	store := &scheduleMemoryStore{}
	response := httptest.NewRecorder()
	data := "homeTeam,awayTeam,seasonIndex,stageIndex,weekIndex\nBills,Jets,0,0,0\nGiants,Eagles,0,2,18\n"
	NewScheduleHandler(store).Upload(response, scheduleUploadRequest(t, data))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %q", response.Code, response.Body.String())
	}
	want := []Game{
		{SeasonIndex: 0, Stage: false, WeekIndex: 0, Home: "Bills", Away: "Jets"},
		{SeasonIndex: 0, Stage: true, WeekIndex: 18, Home: "Giants", Away: "Eagles"},
	}
	if !reflect.DeepEqual(store.games, want) {
		t.Fatalf("stored games = %#v, want %#v", store.games, want)
	}
}

func TestScheduleUploadReturnsDatabaseError(t *testing.T) {
	store := &scheduleMemoryStore{err: errors.New("database unavailable")}
	response := httptest.NewRecorder()
	data := "homeTeam,awayTeam,seasonIndex,stageIndex,weekIndex\nBills,Jets,0,1,7\n"
	NewScheduleHandler(store).Upload(response, scheduleUploadRequest(t, data))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %q", response.Code, response.Body.String())
	}
}

type scheduleMemoryStore struct {
	games []Game
	err   error
}

func (s *scheduleMemoryStore) UpsertGame(_ context.Context, game Game) error {
	if s.err != nil {
		return s.err
	}
	s.games = append(s.games, game)
	return nil
}

func scheduleUploadRequest(t *testing.T, data string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("MEGA_games.csv", "games.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/schedule/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}
