package model

type TargetMessage struct {
	Action string
	Value  string
	Image  []byte
}

type DiscordMessage struct {
	MessageId   string
	Proceed     bool
	NewWeek     bool
	NewWeekText string
	Games       []DiscordGame
}

type DiscordGame struct {
	MessageId  string
	Proceed    bool
	GameNumber string
	GameUrl    string
}
