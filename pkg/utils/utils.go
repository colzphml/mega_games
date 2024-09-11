package utils

import (
	"encoding/csv"
	"io"
	"log"
	"os"
	"strconv"

	"github.com/colzphml/mega_games/internal/model"
)

// Function to read and parse the CSV into the Schedule structure
func ParseScheduleFromCSV(filePath string) model.Schedule {
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Unable to read input file: %v", err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	_, err = csvReader.Read() // Skip the header row
	if err != nil {
		log.Fatalf("Unable to read the header row: %v", err)
	}
	schedule := model.Schedule{}
	seasonMap := make(map[int]int)

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Unable to read a row: %v", err)
		}

		seasonIndex, _ := strconv.Atoi(record[9])
		weekIndex, _ := strconv.Atoi(record[11])
		stageIndex, _ := strconv.Atoi(record[10])
		stage := stageIndex > 0

		game := model.Game{
			Season: seasonIndex,
			Week:   weekIndex,
			Stage:  stage,
			Home:   record[3],
			Away:   record[4],
		}

		if _, exists := seasonMap[seasonIndex]; !exists {
			newSeason := model.Season{Index: seasonIndex}
			schedule.Season = append(schedule.Season, newSeason)
			seasonMap[seasonIndex] = len(schedule.Season) - 1
		}
		seasonIdx := seasonMap[seasonIndex]

		var weekExistFlag bool
		for weekIdx, week := range schedule.Season[seasonIdx].Weeks {
			if week.Index == weekIndex && week.Stage == stage {
				schedule.Season[seasonIdx].Weeks[weekIdx].Games = append(schedule.Season[seasonIdx].Weeks[weekIdx].Games, game)
				weekExistFlag = true
				break
			}
		}
		if !weekExistFlag {
			newWeek := model.Week{
				Index: weekIndex,
				Stage: stage,
				Games: []model.Game{game},
			}
			schedule.Season[seasonIdx].Weeks = append(schedule.Season[seasonIdx].Weeks, newWeek)
		}
	}

	return schedule
}

func GetLastSeason(schedule model.Schedule) model.Season {
	maxSeasonIndex := -1
	for i, season := range schedule.Season {
		if season.Index > maxSeasonIndex {
			maxSeasonIndex = i
		}
	}
	result := schedule.Season[maxSeasonIndex]
	return result
}

func GetWeek(season model.Season, stage bool, weekIndex int) model.Week {
	for _, week := range season.Weeks {
		if week.Index == weekIndex && week.Stage == stage {
			return week
		}
	}
	return model.Week{}
}

func GetAllGamesForWeek(schedule model.Schedule, stage bool, weekIndex int) []model.Game {
	correctWeek := weekIndex - 1
	season := GetLastSeason(schedule)
	week := GetWeek(season, stage, correctWeek)
	return week.Games
}

// ParseCSVFileToTeams reads a CSV file and parses it into a Teams structure.
func ParseCSVFileToTeams(filePath string) (model.Teams, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return model.Teams{}, err
	}
	defer file.Close()

	r := csv.NewReader(file)
	teams := model.Teams{
		Teams: make(map[string]model.Team),
	}

	// Read the header row.
	if _, err := r.Read(); err != nil {
		return model.Teams{}, err
	}

	// Read each record.
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return model.Teams{}, err
		}

		team := model.Team{
			Name:      record[0],
			ShortName: record[2],
			Emoji:     record[3],
			Player:    record[1],
		}
		teams.Teams[team.Name] = team
	}
	return teams, nil
}
