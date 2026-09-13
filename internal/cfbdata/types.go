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

// APITeamSP is one team's SP+ rating from /ratings/sp.
//
// The nested objects carry far more fields than these -- success, explosiveness,
// havoc, rushing and passing splits -- and every one of them is null on this
// tier, for a completed season as readily as a live one. Only what the endpoint
// actually populates is modelled.
type APITeamSP struct {
	Year       int      `json:"year"`
	Team       string   `json:"team"`
	Conference string   `json:"conference"`
	Rating     *float64 `json:"rating"`
	Ranking    *int     `json:"ranking"`
	Offense    struct {
		Rating  *float64 `json:"rating"`
		Ranking *int     `json:"ranking"`
	} `json:"offense"`
	Defense struct {
		Rating  *float64 `json:"rating"`
		Ranking *int     `json:"ranking"`
	} `json:"defense"`
	SpecialTeams struct {
		Rating *float64 `json:"rating"`
	} `json:"specialTeams"`
}

// APINationalAveragesTeam is the synthetic team name /ratings/sp uses for its
// league-wide averages row. It is not a team, will never resolve to one, and is
// skipped by name so it does not sit in the unmatched-team warning every run.
const APINationalAveragesTeam = "nationalAverages"

// APITeamFPI is one team's Football Power Index rating from /ratings/fpi.
type APITeamFPI struct {
	Year        int      `json:"year"`
	Team        string   `json:"team"`
	Conference  string   `json:"conference"`
	FPI         *float64 `json:"fpi"`
	ResumeRanks struct {
		StrengthOfRecord   *int `json:"strengthOfRecord"`
		FPI                *int `json:"fpi"`
		StrengthOfSchedule *int `json:"strengthOfSchedule"`
	} `json:"resumeRanks"`
	Efficiencies struct {
		Overall      *float64 `json:"overall"`
		Offense      *float64 `json:"offense"`
		Defense      *float64 `json:"defense"`
		SpecialTeams *float64 `json:"specialTeams"`
	} `json:"efficiencies"`
}

// APITeamCore is one team's CORE rating from /ratings/core.
//
// Alone among the rating endpoints it says what it has seen, in ThroughWeek and
// ModelVersion, which is why those two columns exist and are null for the others.
type APITeamCore struct {
	Year         int      `json:"year"`
	Team         string   `json:"team"`
	Conference   string   `json:"conference"`
	Overall      *float64 `json:"overall"`
	Offense      *float64 `json:"offense"`
	Defense      *float64 `json:"defense"`
	ThroughWeek  *int     `json:"throughWeek"`
	ModelVersion string   `json:"modelVersion"`
}

// APIRecordSplit is one won-lost-tied split within APITeamRecords.
type APIRecordSplit struct {
	Games  int `json:"games"`
	Wins   int `json:"wins"`
	Losses int `json:"losses"`
	Ties   int `json:"ties"`
}

// APITeamRecords is one team's season record from /records.
//
// The endpoint returns conference, neutral-site, regular-season and postseason
// splits as well; only the three the page shows are decoded.
type APITeamRecords struct {
	Year         int            `json:"year"`
	TeamID       int64          `json:"teamId"`
	Team         string         `json:"team"`
	Conference   string         `json:"conference"`
	ExpectedWins *float64       `json:"expectedWins"`
	Total        APIRecordSplit `json:"total"`
	HomeGames    APIRecordSplit `json:"homeGames"`
	AwayGames    APIRecordSplit `json:"awayGames"`
}

// APITeamATS is one team's against-the-spread record from /teams/ats.
//
// Like /records and unlike the rating endpoints, this supplies a teamId, so a
// school rename cannot lose a row. Coverage is broader than the ratings too --
// 254 rows for 2026 against 138 -- because it covers every team anyone has
// posted a line on rather than every team in a division.
type APITeamATS struct {
	Year           int      `json:"year"`
	TeamID         int64    `json:"teamId"`
	Team           string   `json:"team"`
	Conference     string   `json:"conference"`
	Games          int      `json:"games"`
	ATSWins        int      `json:"atsWins"`
	ATSLosses      int      `json:"atsLosses"`
	ATSPushes      int      `json:"atsPushes"`
	AvgCoverMargin *float64 `json:"avgCoverMargin"`
}

// APIPregameWP is one game's pre-game win probability from
// /metrics/wp/pregame.
//
// GameID is the provider's game id, matching Game.ExternalID, so this needs no
// team resolution at all.
type APIPregameWP struct {
	Season             int      `json:"season"`
	Week               int      `json:"week"`
	SeasonType         string   `json:"seasonType"`
	GameID             int64    `json:"gameId"`
	HomeTeam           string   `json:"homeTeam"`
	AwayTeam           string   `json:"awayTeam"`
	Spread             *float64 `json:"spread"`
	HomeWinProbability *float64 `json:"homeWinProbability"`
}

// APIAdvancedSide is one side's season efficiency within
// APITeamAdvancedStats.
//
// The endpoint returns roughly forty numbers per side. Only the ones the page
// shows are decoded, on the same principle as APITeamSP: an unused field is a
// column somebody eventually feels obliged to render.
//
// Havoc is nested one level deeper, and PointsPerOpportunity sits beside a
// totalOpportunies key whose spelling is the provider's and not a typo here --
// it is not decoded, which is the only reason that does not matter.
type APIAdvancedSide struct {
	Plays                *int     `json:"plays"`
	Drives               *int     `json:"drives"`
	PPA                  *float64 `json:"ppa"`
	SuccessRate          *float64 `json:"successRate"`
	Explosiveness        *float64 `json:"explosiveness"`
	LineYards            *float64 `json:"lineYards"`
	PointsPerOpportunity *float64 `json:"pointsPerOpportunity"`
	Havoc                struct {
		Total *float64 `json:"total"`
	} `json:"havoc"`
}

// APITeamAdvancedStats is one team's season efficiency from
// /stats/season/advanced.
//
// FBS only, with or without a classification parameter -- the unfiltered call
// returns the same 138 rows. It carries no teamId, so resolution is by name as
// it is for the ratings.
type APITeamAdvancedStats struct {
	Season     int             `json:"season"`
	Team       string          `json:"team"`
	Conference string          `json:"conference"`
	Offense    APIAdvancedSide `json:"offense"`
	Defense    APIAdvancedSide `json:"defense"`
}

// APIGameWeather is one game's forecast from /games/weather.
//
// ID is the provider's game id, matching Game.ExternalID.
//
// GameIndoors is the field that matters most and is easiest to ignore: a domed
// stadium returns a complete, plausible forecast for the weather outside the
// roof, wind speed and all.
type APIGameWeather struct {
	ID                   int64     `json:"id"`
	Season               int       `json:"season"`
	Week                 int       `json:"week"`
	SeasonType           string    `json:"seasonType"`
	StartTime            time.Time `json:"startTime"`
	GameIndoors          bool      `json:"gameIndoors"`
	HomeTeam             string    `json:"homeTeam"`
	AwayTeam             string    `json:"awayTeam"`
	Venue                string    `json:"venue"`
	Temperature          *float64  `json:"temperature"`
	Humidity             *int      `json:"humidity"`
	Precipitation        *float64  `json:"precipitation"`
	Snowfall             *float64  `json:"snowfall"`
	WindSpeed            *float64  `json:"windSpeed"`
	WeatherCondition     *string   `json:"weatherCondition"`
	WeatherConditionCode *int      `json:"weatherConditionCode"`
}
