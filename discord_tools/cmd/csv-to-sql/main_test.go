package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadScheduleUsesNamedColumns(t *testing.T) {
	want := []gameRow{{SeasonIndex: 0, Stage: true, WeekIndex: 7, HomeTeam: "Bills", AwayTeam: "Jets"}}
	for _, tc := range []struct {
		name string
		csv  string
	}{
		{
			name: "old export",
			csv:  "id,gameId,league,homeTeam,awayTeam,awayScore,homeScore,isGameOfTheWeek,manual_gotw,seasonIndex,stageIndex,weekIndex,status\n1,42,MEGA,Bills,Jets,0,0,false,false,0,1,7,scheduled\n",
		},
		{
			name: "new export with gotw_reason",
			csv:  "id,gameId,league,homeTeam,awayTeam,awayScore,homeScore,isGameOfTheWeek,manual_gotw,gotw_reason,seasonIndex,stageIndex,weekIndex,status\n1,42,MEGA,Bills,Jets,0,0,false,false,rivalry,0,1,7,scheduled\n",
		},
		{
			name: "reordered columns with extras",
			csv:  "weekIndex,gotw_reason,awayTeam,stageIndex,homeTeam,seasonIndex\n7,rivalry,Jets,1,Bills,0\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readSchedule(writeSchedule(t, tc.csv))
			if err != nil {
				t.Fatalf("readSchedule: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("games = %#v, want %#v", got, want)
			}
		})
	}
}

func TestReadScheduleRejectsInvalidInput(t *testing.T) {
	header := "homeTeam,awayTeam,seasonIndex,stageIndex,weekIndex\n"
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
		{"invalid season", header + "Bills,Jets,no,1,7\n", []string{"row 2", "seasonIndex"}},
		{"missing season", header + "Bills,Jets,,1,7\n", []string{"row 2", "seasonIndex"}},
		{"negative season", header + "Bills,Jets,-1,1,7\n", []string{"row 2", "seasonIndex"}},
		{"invalid stage", header + "Bills,Jets,0,regular,7\n", []string{"row 2", "stageIndex"}},
		{"missing stage", header + "Bills,Jets,0,,7\n", []string{"row 2", "stageIndex"}},
		{"negative stage", header + "Bills,Jets,0,-1,7\n", []string{"row 2", "stageIndex"}},
		{"invalid week", header + "Bills,Jets,0,1,week7\n", []string{"row 2", "weekIndex"}},
		{"missing week", header + "Bills,Jets,0,1,\n", []string{"row 2", "weekIndex"}},
		{"negative week", header + "Bills,Jets,0,1,-1\n", []string{"row 2", "weekIndex"}},
		{"missing home team", header + ",Jets,0,1,7\n", []string{"row 2", "homeTeam"}},
		{"blank away team", header + "Bills,   ,0,1,7\n", []string{"row 2", "awayTeam"}},
		{"short row", header + "Bills,Jets,0,1\n", []string{"row 2"}},
		{"broken quoting", header + "\"Bills,Jets,0,1,7\n", []string{"row 2"}},
		{"invalid late row", header + "Bills,Jets,0,1,7\nGiants,Eagles,0,1,bad\n", []string{"row 3", "weekIndex"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			games, err := readSchedule(writeSchedule(t, tc.csv))
			if err == nil {
				t.Fatalf("readSchedule accepted invalid CSV: %#v", games)
			}
			if len(games) != 0 {
				t.Fatalf("invalid CSV returned partial games: %#v", games)
			}
			for _, fragment := range tc.context {
				if !strings.Contains(err.Error(), fragment) {
					t.Errorf("error %q lacks context %q", err, fragment)
				}
			}
		})
	}
}

func TestReadSchedulePreservesStageMapping(t *testing.T) {
	games, err := readSchedule(writeSchedule(t, "homeTeam,awayTeam,seasonIndex,stageIndex,weekIndex\nBills,Jets,0,0,0\nGiants,Eagles,0,2,18\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []gameRow{
		{SeasonIndex: 0, Stage: false, WeekIndex: 0, HomeTeam: "Bills", AwayTeam: "Jets"},
		{SeasonIndex: 0, Stage: true, WeekIndex: 18, HomeTeam: "Giants", AwayTeam: "Eagles"},
	}
	if !reflect.DeepEqual(games, want) {
		t.Fatalf("games = %#v, want %#v", games, want)
	}
}

func TestReadScheduleAcceptsPostgresIntegerMaximum(t *testing.T) {
	data := "homeTeam,awayTeam,seasonIndex,stageIndex,weekIndex\nBills,Jets,2147483647,2147483648,2147483647\n"
	games, err := readSchedule(writeSchedule(t, data))
	if err != nil {
		t.Fatal(err)
	}
	want := []gameRow{{SeasonIndex: 2147483647, Stage: true, WeekIndex: 2147483647, HomeTeam: "Bills", AwayTeam: "Jets"}}
	if !reflect.DeepEqual(games, want) {
		t.Fatalf("games = %#v, want %#v", games, want)
	}
}

func TestReadScheduleRejectsPostgresIntegerOverflow(t *testing.T) {
	headerAndValidRow := "homeTeam,awayTeam,seasonIndex,stageIndex,weekIndex\nBills,Jets,0,1,7\n"
	for _, tc := range []struct {
		field string
		row   string
	}{
		{"seasonIndex", "Giants,Eagles,2147483648,1,7\n"},
		{"weekIndex", "Giants,Eagles,0,1,2147483648\n"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			games, err := readSchedule(writeSchedule(t, headerAndValidRow+tc.row))
			if err == nil {
				t.Fatalf("accepted INT overflow: %#v", games)
			}
			if len(games) != 0 {
				t.Fatalf("invalid CSV returned partial games: %#v", games)
			}
			for _, fragment := range []string{"row 3", tc.field} {
				if !strings.Contains(err.Error(), fragment) {
					t.Errorf("error %q lacks context %q", err, fragment)
				}
			}
		})
	}
}

func writeSchedule(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "games.csv")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
