package model

type Team struct {
	Name      string
	ShortName string
	Emoji     string
	Player    string
}

type Teams struct {
	Teams map[string]Team
}
