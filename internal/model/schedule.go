package model

type Season struct {
	Index int
	Weeks []Week
}

type Week struct {
	Index int
	Stage bool
	Games []Game
}

type Game struct {
	Season int
	Week   int
	Stage  bool
	Home   string
	Away   string
}

type Schedule struct {
	Season []Season
}
