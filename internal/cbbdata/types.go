package cbbdata

import (
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
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

// APIMadeAttempted is a made-and-attempted shooting line.
type APIMadeAttempted struct {
	Made      int `json:"made"`
	Attempted int `json:"attempted"`
}

// APILineupSide is one side's totals while a five-man unit was on the floor.
type APILineupSide struct {
	Points            int              `json:"points"`
	Possessions       int              `json:"possessions"`
	FieldGoals        APIMadeAttempted `json:"fieldGoals"`
	ThreePointers     APIMadeAttempted `json:"threePointers"`
	FreeThrows        APIMadeAttempted `json:"freeThrows"`
	Turnovers         int              `json:"turnovers"`
	OffensiveRebounds int              `json:"offensiveRebounds"`
	DefensiveRebounds int              `json:"defensiveRebounds"`
}

// APIGameLineup is one five-man unit's stats in one game. IDHash is the unit's
// player IDs joined in order, so it is the same unit across games.
type APIGameLineup struct {
	TeamID       int64                 `json:"teamId"`
	IDHash       string                `json:"idHash"`
	Athletes     models.LineupAthletes `json:"athletes"`
	TotalSeconds float64               `json:"totalSeconds"`
	NetRating    float64               `json:"netRating"`
	TeamStats    APILineupSide         `json:"teamStats"`
	Opponent     APILineupSide         `json:"opponentStats"`
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

// APITeamSeasonSide is what /stats/team/season reports for one end of the floor.
//
// turnoverRatio is left out on purpose. It is a fraction here (0.16) and a percentage
// in /games/teams (21.7), so TurnoverPct works it out from the counts instead.
type APITeamSeasonSide struct {
	Possessions *float64 `json:"possessions"`
	Rating      *float64 `json:"rating"`
	ThreePoint  struct {
		Pct *float64 `json:"pct"`
	} `json:"threePointFieldGoals"`
	Turnovers struct {
		Total *float64 `json:"total"`
	} `json:"turnovers"`
	FourFactors struct {
		EffectiveFGPct      *float64 `json:"effectiveFieldGoalPct"`
		OffensiveReboundPct *float64 `json:"offensiveReboundPct"`
		FreeThrowRate       *float64 `json:"freeThrowRate"`
	} `json:"fourFactors"`
}

// TurnoverPct is turnovers per 100 possessions, nil when either count is missing.
func (s APITeamSeasonSide) TurnoverPct() *float64 {
	if s.Turnovers.Total == nil || s.Possessions == nil || *s.Possessions <= 0 {
		return nil
	}
	pct := *s.Turnovers.Total * 100 / *s.Possessions
	return &pct
}

// APITeamSeasonStats is one team's row from /stats/team/season.
type APITeamSeasonStats struct {
	TeamID        int64             `json:"teamId"`
	Games         float64           `json:"games"`
	Wins          float64           `json:"wins"`
	Losses        float64           `json:"losses"`
	Pace          *float64          `json:"pace"`
	TeamStats     APITeamSeasonSide `json:"teamStats"`
	OpponentStats APITeamSeasonSide `json:"opponentStats"`
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
