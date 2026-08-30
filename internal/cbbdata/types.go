package cbbdata

import "time"

// APIVenue represents a venue from the CBB Data API.
type APIVenue struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	City    string `json:"city"`
	State   string `json:"state"`
	Country string `json:"country"`
}

// APITeam represents a team from the CBB Data API.
type APITeam struct {
	ID             int64  `json:"id"`
	School         string `json:"school"`
	Mascot         string `json:"mascot"`
	Abbreviation   string `json:"abbreviation"`
	PrimaryColor   string `json:"primaryColor"`
	SecondaryColor string `json:"secondaryColor"`
	CurrentVenueID *int64 `json:"currentVenueId"`
	CurrentVenue   string `json:"currentVenue"`
	CurrentCity    string `json:"currentCity"`
	CurrentState   string `json:"currentState"`
	Conference     string `json:"conference"`
}

// APIGame represents a game from the CBB Data API.
type APIGame struct {
	ID               int64     `json:"id"`
	Season           int       `json:"season"`
	SeasonType       string    `json:"seasonType"`
	StartDate        time.Time `json:"startDate"`
	StartTimeTBD     bool      `json:"startTimeTbd"`
	NeutralSite      bool      `json:"neutralSite"`
	ConferenceGame   bool      `json:"conferenceGame"`
	GameType         *string   `json:"gameType"`
	Tournament       *string   `json:"tournament"`
	Status           string    `json:"status"`
	HomeTeamID       int64     `json:"homeTeamId"`
	HomeTeam         string    `json:"homeTeam"`
	HomeConference   *string   `json:"homeConference"`
	HomeSeed         *int      `json:"homeSeed"`
	HomePoints       *int      `json:"homePoints"`
	HomePeriodPoints []int     `json:"homePeriodPoints"`
	AwayTeamID       int64     `json:"awayTeamId"`
	AwayTeam         string    `json:"awayTeam"`
	AwayConference   *string   `json:"awayConference"`
	AwaySeed         *int      `json:"awaySeed"`
	AwayPoints       *int      `json:"awayPoints"`
	AwayPeriodPoints []int     `json:"awayPeriodPoints"`
	VenueID          *int64    `json:"venueId"`
	Venue            string    `json:"venue"`
	City             string    `json:"city"`
	State            string    `json:"state"`
	Excitement       *float64  `json:"excitement"`
}

// APILineProvider represents betting lines from a specific provider.
type APILineProvider struct {
	Provider      string   `json:"provider"`
	Spread        *float64 `json:"spread"`
	OverUnder     *float64 `json:"overUnder"`
	HomeMoneyline *float64 `json:"homeMoneyline"`
	AwayMoneyline *float64 `json:"awayMoneyline"`
}

// APIGameLines represents betting lines for a game from the CBB Data API.
type APIGameLines struct {
	GameID         int64             `json:"gameId"`
	Season         int               `json:"season"`
	SeasonType     string            `json:"seasonType"`
	StartDate      time.Time         `json:"startDate"`
	HomeTeamID     int64             `json:"homeTeamId"`
	HomeTeam       string            `json:"homeTeam"`
	HomeConference *string           `json:"homeConference"`
	HomeScore      *float64          `json:"homeScore"`
	AwayTeamID     int64             `json:"awayTeamId"`
	AwayTeam       string            `json:"awayTeam"`
	AwayConference *string           `json:"awayConference"`
	AwayScore      *float64          `json:"awayScore"`
	Lines          []APILineProvider `json:"lines"`
}

// GameQueryOpts holds optional query parameters for the games endpoint.
type GameQueryOpts struct {
	Season         *int
	SeasonType     *string
	StartDateRange *string
	EndDateRange   *string
	Team           *string
	Conference     *string
	Tournament     *string
	Status         *string
}

// LineQueryOpts holds optional query parameters for the lines endpoint.
type LineQueryOpts struct {
	Season         *int
	StartDateRange *string
	EndDateRange   *string
	Team           *string
	Conference     *string
}
