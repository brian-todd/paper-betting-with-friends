package models

import (
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Periods a game runs before overtime: four quarters of football, two halves of
// basketball.
const (
	footballRegulationPeriods   = 4
	basketballRegulationPeriods = 2
)

// regulationPeriods is how many periods sport plays before overtime. An
// unrecognised sport is read as football, which is what every caller that does
// not resolve one is.
func regulationPeriods(sport string) int {
	if sport == SportBasketball {
		return basketballRegulationPeriods
	}
	return footballRegulationPeriods
}

// GameLiveState is what the scoreboard feed reports about a game beyond its
// score: the clock, the situation, the broadcast and the weather.
//
// A row exists as soon as the scoreboard has seen the game, which happens well
// before kickoff, so its presence says nothing about whether the game is under
// way -- read Game.Status for that. The score lives in GameResult, which owns
// the provisional/final distinction that bet settlement keys off.
type GameLiveState struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	GameID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex"`

	// Period and Clock are the game clock as of the last sync. They are nil
	// outside a live game and are left in place once it ends, so neither is a
	// signal that the game is running.
	Period *int
	Clock  *string `gorm:"type:varchar(16)"`

	// Situation and Possession are provider prose -- down and distance, and
	// whichever wording the feed uses for the side with the ball. Nothing here
	// parses them; Possession is matched loosely for display and shown as-is
	// when it does not resolve.
	Situation  *string `gorm:"type:text"`
	Possession *string `gorm:"type:varchar(16)"`
	LastPlay   *string `gorm:"type:text"`

	// TV can name several networks at once ("ESPN | Disney+"), so it stays a
	// display string rather than becoming an enum.
	TV *string `gorm:"type:varchar(255)"`

	WeatherDescription *string          `gorm:"type:varchar(255)"`
	Temperature        *decimal.Decimal `gorm:"type:decimal(5,1)"`
	WindSpeed          *decimal.Decimal `gorm:"type:decimal(5,1)"`
	WindDirection      *int

	// HomeWinProbability is the live probability the home side wins, 0 to 1.
	// The feed reports one per team but they are complementary, so one column
	// carries both -- see AwayWinProbability.
	HomeWinProbability *decimal.Decimal `gorm:"type:decimal(5,4)"`

	CreatedAt time.Time
	UpdatedAt time.Time

	// Relationships.
	Game Game `gorm:"foreignKey:GameID"`
}

// BeforeCreate sets the UUID before creating a new live state.
func (s *GameLiveState) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

// PeriodLabel names the period for display: "Q3", "OT", "2OT". It is empty when
// the feed has not reported one, which is every game that is not under way.
//
// A nil receiver is a game the scoreboard has never covered, so every method
// here answers for one rather than making each template guard the call.
func (s *GameLiveState) PeriodLabel() string {
	if s == nil || s.Period == nil || *s.Period < 1 {
		return ""
	}
	// The scoreboard feed is football only, so there is no sport to resolve.
	return periodLabel(SportFootball, *s.Period)
}

// periodLabel names the nth period of a game. It is shared with the line score,
// so the column headed "OT" there and the badge reading "OT" on the same page
// cannot drift apart.
//
// Football runs four quarters, basketball two halves, and everything past
// regulation is overtime in both.
func periodLabel(sport string, period int) string {
	regulation := regulationPeriods(sport)
	switch {
	case period < 1:
		return ""
	case period <= regulation && sport == SportBasketball:
		return strconv.Itoa(period) + "H"
	case period <= regulation:
		return "Q" + strconv.Itoa(period)
	case period == regulation+1:
		return "OT"
	default:
		return strconv.Itoa(period-regulation) + "OT"
	}
}

// ClockText is the game clock, empty when there is none to show.
func (s *GameLiveState) ClockText() string {
	if s == nil || s.Clock == nil {
		return ""
	}
	return strings.TrimSpace(*s.Clock)
}

// LiveLabel is the game clock as one string for a status badge -- "Q3 · 7:42",
// or just "Q3" when the feed has the period but not the clock. It is empty when
// there is neither, which is what a caller shows a plain "Live" for.
func (s *GameLiveState) LiveLabel() string {
	period, clock := s.PeriodLabel(), s.ClockText()
	switch {
	case period != "" && clock != "":
		return period + " · " + clock
	case period != "":
		return period
	default:
		return clock
	}
}

// SituationText is the down and distance, empty when the feed reported none.
func (s *GameLiveState) SituationText() string {
	if s == nil || s.Situation == nil {
		return ""
	}
	return strings.TrimSpace(*s.Situation)
}

// LastPlayText is the most recent play description, empty when there is none.
func (s *GameLiveState) LastPlayText() string {
	if s == nil || s.LastPlay == nil {
		return ""
	}
	return strings.TrimSpace(*s.LastPlay)
}

// Broadcast is the TV listing, empty when the feed reported none.
func (s *GameLiveState) Broadcast() string {
	if s == nil || s.TV == nil {
		return ""
	}
	return strings.TrimSpace(*s.TV)
}

// HomeHasBall and AwayHasBall report which side the feed says has possession.
//
// The value is matched rather than parsed: the field is documented only as
// naming the team with the ball, and it has been observed empty for every game
// checked, so anything that is not recognisably one side or the other has to
// resolve to neither rather than to a guess.
func (s *GameLiveState) HomeHasBall() bool { return s.possessionIs("home") }

// AwayHasBall reports whether the away side has possession.
func (s *GameLiveState) AwayHasBall() bool { return s.possessionIs("away") }

func (s *GameLiveState) possessionIs(side string) bool {
	if s == nil || s.Possession == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(*s.Possession), side)
}

// WeatherText is a one-line summary such as "Light Rain, 79°F", empty when the
// feed reported neither half of it.
func (s *GameLiveState) WeatherText() string {
	if s == nil {
		return ""
	}

	var parts []string
	if s.WeatherDescription != nil && strings.TrimSpace(*s.WeatherDescription) != "" {
		parts = append(parts, strings.TrimSpace(*s.WeatherDescription))
	}
	if s.Temperature != nil {
		parts = append(parts, s.Temperature.Round(0).String()+"°F")
	}
	return strings.Join(parts, ", ")
}

// WindText describes the wind as "7 mph WNW", empty without a speed. The
// direction is the bearing the wind blows *from*, which is the convention every
// weather report uses and the one a reader will assume.
func (s *GameLiveState) WindText() string {
	if s == nil || s.WindSpeed == nil {
		return ""
	}

	text := s.WindSpeed.Round(0).String() + " mph"
	if compass := compassPoint(s.WindDirection); compass != "" {
		text += " " + compass
	}
	return text
}

// compassPoints are the sixteen headings, starting at north and running
// clockwise, so an index derived from the bearing selects directly.
var compassPoints = [16]string{
	"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
	"S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW",
}

// compassPoint converts a bearing in degrees to its nearest sixteenth. Bearings
// outside 0-360 are normalised rather than rejected: the feed is not ours, and
// a wrapped heading is still a heading.
func compassPoint(degrees *int) string {
	if degrees == nil {
		return ""
	}

	// Each point spans 22.5 degrees, so half a span is added before dividing to
	// round to the nearest rather than always down. The arithmetic is in
	// tenths of a degree to keep it in integers.
	const spanTenths = 225
	tenths := ((*degrees*10)%3600 + 3600) % 3600
	return compassPoints[((tenths+spanTenths/2)/spanTenths)%16]
}

// HasLiveDetail reports whether there is anything worth drawing a live strip
// for: a clock, a period, a situation or a last play.
func (s *GameLiveState) HasLiveDetail() bool {
	if s == nil {
		return false
	}
	return s.PeriodLabel() != "" || s.ClockText() != "" || s.SituationText() != "" || s.LastPlayText() != ""
}

// AwayWinProbability is the complement of the home side's, nil when the feed
// reported none. It is derived rather than stored so the two can never
// disagree.
func (s *GameLiveState) AwayWinProbability() *decimal.Decimal {
	if s == nil || s.HomeWinProbability == nil {
		return nil
	}
	away := decimal.NewFromInt(1).Sub(*s.HomeWinProbability)
	return &away
}
