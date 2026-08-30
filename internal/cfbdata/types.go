package cfbdata

import "time"

// APIVenue represents a venue from the CFB Data API.
type APIVenue struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	City             string  `json:"city"`
	State            string  `json:"state"`
	Zip              string  `json:"zip"`
	CountryCode      string  `json:"countryCode"`
	Timezone         string  `json:"timezone"`
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	Elevation        string  `json:"elevation"`
	Capacity         int     `json:"capacity"`
	ConstructionYear int     `json:"constructionYear"`
	Grass            bool    `json:"grass"`
	Dome             bool    `json:"dome"`
}

// APITeamLocation represents venue info embedded in team data.
type APITeamLocation struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	City             string  `json:"city"`
	State            string  `json:"state"`
	Zip              string  `json:"zip"`
	CountryCode      string  `json:"countryCode"`
	Timezone         string  `json:"timezone"`
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	Elevation        string  `json:"elevation"`
	Capacity         int     `json:"capacity"`
	ConstructionYear int     `json:"constructionYear"`
	Grass            bool    `json:"grass"`
	Dome             bool    `json:"dome"`
}

// APITeam represents a team from the CFB Data API.
type APITeam struct {
	ID             int64            `json:"id"`
	School         string           `json:"school"`
	Mascot         string           `json:"mascot"`
	Abbreviation   string           `json:"abbreviation"`
	AlternateNames []string         `json:"alternateNames"`
	Conference     string           `json:"conference"`
	Division       string           `json:"division"`
	Classification string           `json:"classification"`
	Color          string           `json:"color"`
	AlternateColor string           `json:"alternateColor"`
	Logos          []string         `json:"logos"`
	Twitter        string           `json:"twitter"`
	Location       *APITeamLocation `json:"location"`
}

// APIWeek represents a calendar week from the CFB Data API.
type APIWeek struct {
	Season     int       `json:"season"`
	Week       int       `json:"week"`
	SeasonType string    `json:"seasonType"`
	StartDate  time.Time `json:"startDate"`
	EndDate    time.Time `json:"endDate"`
}

// APIGame represents a game from the CFB Data API.
type APIGame struct {
	ID                         int64     `json:"id"`
	Season                     int       `json:"season"`
	Week                       int       `json:"week"`
	SeasonType                 string    `json:"seasonType"`
	StartDate                  time.Time `json:"startDate"`
	StartTimeTBD               bool      `json:"startTimeTBD"`
	Completed                  bool      `json:"completed"`
	NeutralSite                bool      `json:"neutralSite"`
	ConferenceGame             bool      `json:"conferenceGame"`
	Attendance                 int       `json:"attendance"`
	VenueID                    int64     `json:"venueId"`
	Venue                      string    `json:"venue"`
	HomeID                     int64     `json:"homeId"`
	HomeTeam                   string    `json:"homeTeam"`
	HomeConference             string    `json:"homeConference"`
	HomeClassification         string    `json:"homeClassification"`
	HomePoints                 *int      `json:"homePoints"`
	HomeLineScores             []int     `json:"homeLineScores"`
	HomePostgameWinProbability *float64  `json:"homePostgameWinProbability"`
	HomePregameElo             *int      `json:"homePregameElo"`
	HomePostgameElo            *int      `json:"homePostgameElo"`
	AwayID                     int64     `json:"awayId"`
	AwayTeam                   string    `json:"awayTeam"`
	AwayConference             string    `json:"awayConference"`
	AwayClassification         string    `json:"awayClassification"`
	AwayPoints                 *int      `json:"awayPoints"`
	AwayLineScores             []int     `json:"awayLineScores"`
	AwayPostgameWinProbability *float64  `json:"awayPostgameWinProbability"`
	AwayPregameElo             *int      `json:"awayPregameElo"`
	AwayPostgameElo            *int      `json:"awayPostgameElo"`
	ExcitementIndex            *float64  `json:"excitementIndex"`
	Highlights                 string    `json:"highlights"`
	Notes                      string    `json:"notes"`
}

// APILineProvider represents betting lines from a specific provider.
type APILineProvider struct {
	Provider        string   `json:"provider"`
	Spread          *float64 `json:"spread"`
	FormattedSpread string   `json:"formattedSpread"`
	SpreadOpen      *float64 `json:"spreadOpen"`
	OverUnder       *float64 `json:"overUnder"`
	OverUnderOpen   *float64 `json:"overUnderOpen"`
	HomeMoneyline   *int     `json:"homeMoneyline"`
	AwayMoneyline   *int     `json:"awayMoneyline"`
}

// APILine represents betting lines for a game from the CFB Data API.
type APILine struct {
	ID                 int64             `json:"id"`
	Season             int               `json:"season"`
	SeasonType         string            `json:"seasonType"`
	Week               int               `json:"week"`
	StartDate          time.Time         `json:"startDate"`
	HomeTeamID         int64             `json:"homeTeamId"`
	HomeTeam           string            `json:"homeTeam"`
	HomeConference     string            `json:"homeConference"`
	HomeClassification string            `json:"homeClassification"`
	HomeScore          *int              `json:"homeScore"`
	AwayTeamID         int64             `json:"awayTeamId"`
	AwayTeam           string            `json:"awayTeam"`
	AwayConference     string            `json:"awayConference"`
	AwayClassification string            `json:"awayClassification"`
	AwayScore          *int              `json:"awayScore"`
	Lines              []APILineProvider `json:"lines"`
}
