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

// APIRankingWeek represents one week's poll rankings from the CFB Data API.
type APIRankingWeek struct {
	Season     int       `json:"season"`
	SeasonType string    `json:"seasonType"`
	Week       int       `json:"week"`
	Polls      []APIPoll `json:"polls"`
}

// APIPoll represents one poll's ranks within a ranking week.
type APIPoll struct {
	Poll  string    `json:"poll"`
	Ranks []APIRank `json:"ranks"`
}

// APIRank represents one team's position within a poll.
//
// TeamID is the same identifier /teams reports, so it resolves against
// teams.external_id the way every other entity in this sync does. School is
// carried for logging only -- it is the readable half of a skipped row.
type APIRank struct {
	Rank            int    `json:"rank"`
	TeamID          int64  `json:"teamId"`
	School          string `json:"school"`
	FirstPlaceVotes *int   `json:"firstPlaceVotes"`
	Points          *int   `json:"points"`
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

// APIScoreboardTeam is one side of a scoreboard game.
//
// Points and LineScores are nil before kickoff, and Points can be nil for one
// side of a game already under way -- the feed reports a side that has not
// scored as null rather than as zero.
type APIScoreboardTeam struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Conference     string   `json:"conference"`
	Classification string   `json:"classification"`
	Points         *int     `json:"points"`
	LineScores     []int    `json:"lineScores"`
	WinProbability *float64 `json:"winProbability"`
}

// APIScoreboardVenue is the venue as the scoreboard reports it: a display name
// and a city, with none of the identifiers /venues carries.
type APIScoreboardVenue struct {
	Name  string `json:"name"`
	City  string `json:"city"`
	State string `json:"state"`
}

// APIScoreboardWeather is the conditions at kickoff. Every field is nullable
// and all four are null together for games the provider has no station for.
type APIScoreboardWeather struct {
	Temperature   *float64 `json:"temperature"`
	Description   *string  `json:"description"`
	WindSpeed     *float64 `json:"windSpeed"`
	WindDirection *int     `json:"windDirection"`
}

// APIScoreboardBetting is the scoreboard's single consensus line.
//
// It is deliberately not synced into the odds tables: those are keyed by book,
// and this line names no provider, so storing it would invent a source. /lines
// remains where odds come from.
type APIScoreboardBetting struct {
	Spread        *float64 `json:"spread"`
	OverUnder     *float64 `json:"overUnder"`
	HomeMoneyline *int     `json:"homeMoneyline"`
	AwayMoneyline *int     `json:"awayMoneyline"`
}

// Statuses reported by the scoreboard feed. Unlike /games, which reports only
// whether a game is completed, this endpoint reports the state directly.
const (
	ScoreboardStatusScheduled  = "scheduled"
	ScoreboardStatusInProgress = "in_progress"
	ScoreboardStatusCompleted  = "completed"
)

// APIScoreboardGame represents a game from the CFB Data API's /scoreboard
// endpoint, which covers the current week only and carries the live clock the
// /games endpoint does not.
type APIScoreboardGame struct {
	ID             int64                 `json:"id"`
	StartDate      time.Time             `json:"startDate"`
	StartTimeTBD   bool                  `json:"startTimeTBD"`
	TV             string                `json:"tv"`
	NeutralSite    bool                  `json:"neutralSite"`
	ConferenceGame bool                  `json:"conferenceGame"`
	Status         string                `json:"status"`
	Period         *int                  `json:"period"`
	Clock          *string               `json:"clock"`
	Situation      *string               `json:"situation"`
	Possession     *string               `json:"possession"`
	LastPlay       *string               `json:"lastPlay"`
	Venue          *APIScoreboardVenue   `json:"venue"`
	HomeTeam       APIScoreboardTeam     `json:"homeTeam"`
	AwayTeam       APIScoreboardTeam     `json:"awayTeam"`
	Weather        *APIScoreboardWeather `json:"weather"`
	Betting        *APIScoreboardBetting `json:"betting"`
}
