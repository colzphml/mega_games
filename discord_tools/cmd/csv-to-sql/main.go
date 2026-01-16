package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func main() {
	teamsPath := flag.String("teams", "", "path to teams CSV (MEGA_teams.csv)")
	schedulePath := flag.String("schedule", "", "path to schedule CSV (MEGA_games.csv)")
	flag.Parse()

	if *teamsPath == "" || *schedulePath == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/csv-to-sql --teams <teams.csv> --schedule <games.csv>")
		os.Exit(2)
	}

	teams, err := readTeams(*teamsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read teams: %v\n", err)
		os.Exit(1)
	}
	games, err := readSchedule(*schedulePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read schedule: %v\n", err)
		os.Exit(1)
	}

	var out strings.Builder
	out.WriteString("BEGIN;\n")

	if len(teams) > 0 {
		out.WriteString("INSERT INTO teams (name, short_name, emoji, player) VALUES\n")
		for i, team := range teams {
			out.WriteString(fmt.Sprintf("('%s','%s','%s','%s')", escape(team.Name), escape(team.ShortName), escape(team.Emoji), escape(team.Player)))
			if i == len(teams)-1 {
				out.WriteString(";\n\n")
			} else {
				out.WriteString(",\n")
			}
		}
	}

	if len(games) > 0 {
		out.WriteString("INSERT INTO schedule_games (season_index, stage, week_index, home_team, away_team) VALUES\n")
		for i, game := range games {
			out.WriteString(fmt.Sprintf("(%d,%t,%d,'%s','%s')", game.SeasonIndex, game.Stage, game.WeekIndex, escape(game.HomeTeam), escape(game.AwayTeam)))
			if i == len(games)-1 {
				out.WriteString(";\n")
			} else {
				out.WriteString(",\n")
			}
		}
	}

	out.WriteString("COMMIT;\n")

	fmt.Print(out.String())
}

type teamRow struct {
	Name      string
	ShortName string
	Emoji     string
	Player    string
}

type gameRow struct {
	SeasonIndex int
	Stage       bool
	WeekIndex   int
	HomeTeam    string
	AwayTeam    string
}

func readTeams(path string) ([]teamRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	if _, err := reader.Read(); err != nil {
		return nil, err
	}

	var teams []teamRow
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(record) < 4 {
			continue
		}
		teams = append(teams, teamRow{
			Name:      record[0],
			Player:    record[1],
			ShortName: record[2],
			Emoji:     record[3],
		})
	}
	return teams, nil
}

func readSchedule(path string) ([]gameRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	if _, err := reader.Read(); err != nil {
		return nil, err
	}

	var games []gameRow
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(record) <= 11 {
			continue
		}
		seasonIndex, err := strconv.Atoi(record[9])
		if err != nil {
			return nil, fmt.Errorf("invalid season index: %w", err)
		}
		stageIndex, err := strconv.Atoi(record[10])
		if err != nil {
			return nil, fmt.Errorf("invalid stage index: %w", err)
		}
		weekIndex, err := strconv.Atoi(record[11])
		if err != nil {
			return nil, fmt.Errorf("invalid week index: %w", err)
		}
		games = append(games, gameRow{
			SeasonIndex: seasonIndex,
			Stage:       stageIndex > 0,
			WeekIndex:   weekIndex,
			HomeTeam:    record[3],
			AwayTeam:    record[4],
		})
	}
	return games, nil
}

func escape(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
