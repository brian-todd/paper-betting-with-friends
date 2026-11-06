package cbbdata

import (
	"time"
)

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
	ID int64 `json:"id"`
	// SourceID is usually a number, and the same one as football's team ID. A few
	// small schools carry a placeholder such as "UNKNOWN#Cotner College".
	SourceID       string `json:"sourceId"`
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
	// The Elo ratings going into the game. Null on a postponed or cancelled one.
	HomeTeamEloStart *int     `json:"homeTeamEloStart"`
	AwayTeamEloStart *int     `json:"awayTeamEloStart"`
	VenueID          *int64   `json:"venueId"`
	Venue            string   `json:"venue"`
	City             string   `json:"city"`
	State            string   `json:"state"`
	Excitement       *float64 `json:"excitement"`
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

// The statuses /scoreboard reports. Football's feed says "completed" where this
// one says "final", so the two packages keep their own.
const (
	scoreboardStatusScheduled  = "scheduled"
	scoreboardStatusInProgress = "in_progress"
	scoreboardStatusFinal      = "final"
	scoreboardStatusPostponed  = "postponed"
	scoreboardStatusCancelled  = "cancelled"
)

// APIScoreboardTeam is one side of a scoreboard game.
type APIScoreboardTeam struct {
	ID         int64 `json:"id"`
	Points     *int  `json:"points"`
	LineScores []int `json:"lineScores"`
}

// APIScoreboardGame is one game from the live scoreboard.
type APIScoreboardGame struct {
	ID           int64             `json:"id"`
	StartDate    time.Time         `json:"startDate"`
	StartTimeTBD bool              `json:"startTimeTbd"`
	TV           string            `json:"tv"`
	Status       string            `json:"status"`
	Period       *int              `json:"period"`
	Clock        *string           `json:"clock"`
	HomeTeam     APIScoreboardTeam `json:"homeTeam"`
	AwayTeam     APIScoreboardTeam `json:"awayTeam"`
}

// APIPollRanking is one team's row in one poll. Ranking is null for a team only
// receiving votes, which is over a third of the rows.
type APIPollRanking struct {
	Season   int        `json:"season"`
	PollDate *time.Time `json:"pollDate"`
	PollType string     `json:"pollType"`
	TeamID   float64    `json:"teamId"`
	Ranking  *float64   `json:"ranking"`
}

// APIAdjustedRating is one team's row from /ratings/adjusted.
type APIAdjustedRating struct {
	TeamID          int64    `json:"teamId"`
	OffensiveRating *float64 `json:"offensiveRating"`
	DefensiveRating *float64 `json:"defensiveRating"`
	NetRating       *float64 `json:"netRating"`
	Rankings        struct {
		Net     *float64 `json:"net"`
		Offense *float64 `json:"offense"`
		Defense *float64 `json:"defense"`
	} `json:"rankings"`
}

// APISRSRating is one team's row from /ratings/srs.
type APISRSRating struct {
	TeamID int64    `json:"teamId"`
	Rating *float64 `json:"rating"`
}

// APIShooting is a made-and-attempted line. The feed types these as nullable doubles.
type APIShooting struct {
	Made      *float64 `json:"made"`
	Attempted *float64 `json:"attempted"`
	Pct       *float64 `json:"pct"`
}

// APITeamSide is what /stats/team/season and /games/teams report for one team, over a
// season and over one game. The two share a shape.
//
// turnoverRatio is left out on purpose. It is a fraction in the first (0.16) and a
// percentage in the second (21.7), so TurnoverPct works it out from the counts instead.
type APITeamSide struct {
	Possessions *float64    `json:"possessions"`
	Rating      *float64    `json:"rating"`
	Assists     *float64    `json:"assists"`
	Blocks      *float64    `json:"blocks"`
	Steals      *float64    `json:"steals"`
	FieldGoals  APIShooting `json:"fieldGoals"`
	ThreePoint  APIShooting `json:"threePointFieldGoals"`
	FreeThrows  APIShooting `json:"freeThrows"`
	Rebounds    struct {
		Total     *float64 `json:"total"`
		Offensive *float64 `json:"offensive"`
		Defensive *float64 `json:"defensive"`
	} `json:"rebounds"`
	Turnovers struct {
		Total *float64 `json:"total"`
	} `json:"turnovers"`
	Fouls struct {
		Total *float64 `json:"total"`
	} `json:"fouls"`
	Points struct {
		Total        *float64 `json:"total"`
		InPaint      *float64 `json:"inPaint"`
		OffTurnovers *float64 `json:"offTurnovers"`
		FastBreak    *float64 `json:"fastBreak"`
	} `json:"points"`
	FourFactors struct {
		EffectiveFGPct      *float64 `json:"effectiveFieldGoalPct"`
		OffensiveReboundPct *float64 `json:"offensiveReboundPct"`
		FreeThrowRate       *float64 `json:"freeThrowRate"`
	} `json:"fourFactors"`
}

// TurnoverPct is turnovers per 100 possessions, nil when either count is missing.
func (s APITeamSide) TurnoverPct() *float64 {
	if s.Turnovers.Total == nil || s.Possessions == nil || *s.Possessions <= 0 {
		return nil
	}
	pct := *s.Turnovers.Total * 100 / *s.Possessions
	return &pct
}

// APITeamSeasonStats is one team's row from /stats/team/season.
type APITeamSeasonStats struct {
	TeamID        int64       `json:"teamId"`
	Games         float64     `json:"games"`
	Wins          float64     `json:"wins"`
	Losses        float64     `json:"losses"`
	Pace          *float64    `json:"pace"`
	TeamStats     APITeamSide `json:"teamStats"`
	OpponentStats APITeamSide `json:"opponentStats"`
}

// APIGameTeamStats is one team's row from /games/teams. A game has one for each team.
type APIGameTeamStats struct {
	GameID    int64       `json:"gameId"`
	TeamID    int64       `json:"teamId"`
	TeamStats APITeamSide `json:"teamStats"`
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
