package games

import (
	"math"
	"strconv"
	"strings"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
)

// The projection is the adjusted-efficiency method the public college basketball
// rating sites use: each side's offense against the other's defense, relative to the
// national average, over the possessions the two paces imply.
//
// It is a statistical estimate and is computed in float64. Nothing here is money or
// odds: it is never stored, priced or settled against, and it leaves this package as
// whole points and a whole percentage.
//
// Both constants were measured on the captured 2025-26 season (5,084 games between
// two rated teams, not at a neutral site), and are one season's measurement rather
// than anything the provider publishes:
//
//   - basketballHomeCourtPoints is the mean margin by which home teams beat this
//     projection without it: 3.0 points. At neutral sites the same residual was 0.2.
//   - projectionMarginSD is the spread of real margins around a projection. It is the
//     sportsbooks' own error on those games, 11.3 points, rounded down to 11, and not
//     this projection's error (10.6): the captured ratings are end-of-season values
//     that already include each game's result, so measuring the projection against
//     the games it learned from would flatter it.
//
// The projected total came out 1.7 points low on the same games, which is left
// uncorrected.
const (
	basketballHomeCourtPoints = 3.0
	projectionMarginSD        = 11.0
)

// pointsText formats one of the constants above for the page's own explanation of
// them, so a re-measured constant cannot leave the text describing the old one.
func pointsText(points float64) string {
	return strconv.FormatFloat(points, 'f', -1, 64)
}

// ScoreProjection is the panel's pregame projection for a basketball game: a score
// for each side, each side's chance of winning, and the posted line beside it.
type ScoreProjection struct {
	// Home and Away are whole points that add up to the rounded projected total, so
	// the total set beside the O/U line is the projection's and not an artefact of
	// rounding each side. See roundScores.
	Home, Away               int
	HomePercent, AwayPercent int

	// Spread and OverUnder are the primary posted line, the same spread the games
	// grid shows and the same book's total where it has one, so the reader can set
	// the two numbers side by side. Nil when no book has posted one, which for a game
	// later in the week is the usual case.
	Spread    *models.SpreadOdds
	OverUnder *models.OverUnderOdds
}

// Total is the projected combined score.
func (p *ScoreProjection) Total() int { return p.Home + p.Away }

// HomeCourtPoints is the home-court adjustment as the footnote states it.
func (p *ScoreProjection) HomeCourtPoints() string { return pointsText(basketballHomeCourtPoints) }

// projectScore projects a basketball game from adjusted efficiency and pace, or
// returns nil when projectionInputs finds nothing to project from.
func projectScore(m *Matchup, baseline repository.AdjustedBaseline) *ScoreProjection {
	home, away, ok := projectionInputs(m)
	if !ok {
		return nil
	}
	return project(home, away, m.NeutralSite, baseline)
}

// projectionInputs is each side's adjusted offense, adjusted defense and pace.
//
// Not ok unless the game is basketball and both sides have all three. Both or
// neither, as with the rating rows: there is no average-team stand-in for a side the
// provider does not rate.
func projectionInputs(m *Matchup) (home, away adjusted, ok bool) {
	if m == nil || m.Sport != models.SportBasketball {
		return adjusted{}, adjusted{}, false
	}
	home, homeOK := adjustedSide(m.Home)
	away, awayOK := adjustedSide(m.Away)
	return home, away, homeOK && awayOK
}

func project(home, away adjusted, neutralSite bool, baseline repository.AdjustedBaseline) *ScoreProjection {
	if baseline.Efficiency.IsZero() || baseline.Pace.IsZero() {
		return nil
	}

	efficiency := baseline.Efficiency.InexactFloat64()
	possessions := home.pace * away.pace / baseline.Pace.InexactFloat64()

	homePoints := home.offense * away.defense / efficiency * possessions / 100
	awayPoints := away.offense * home.defense / efficiency * possessions / 100
	if !neutralSite {
		// Split across the two sides so that home court moves the margin and leaves
		// the total alone.
		homePoints += basketballHomeCourtPoints / 2
		awayPoints -= basketballHomeCourtPoints / 2
	}

	homePercent := winPercent(homePoints - awayPoints)
	homeScore, awayScore := roundScores(homePoints, awayPoints)
	return &ScoreProjection{
		Home:        homeScore,
		Away:        awayScore,
		HomePercent: homePercent,
		AwayPercent: 100 - homePercent,
	}
}

// roundScores turns two projected scores into whole points for the page.
//
// Rounding each side on its own lets the two disagree with what is printed beside
// them: 78.5 and 61.8 round to 79-62, a total of 141, when the projection's is
// 140.3 -- over a 140.5 line that the projection is under. So the total is rounded
// once and shared out by largest remainder, which keeps each side within a point of
// its projection and never puts the underdog ahead.
//
// That can still leave a tie, which basketball does not have and which would sit
// beside a win chance that is not 50%. A tie between two different projections is
// broken a point each way toward the favourite, keeping the total.
func roundScores(home, away float64) (int, int) {
	total := int(math.Round(home + away))
	h, a := int(math.Floor(home)), int(math.Floor(away))
	for h+a < total {
		if home-float64(h) >= away-float64(a) {
			h++
		} else {
			a++
		}
	}
	if h == a {
		switch {
		case home > away:
			h, a = h+1, a-1
		case away > home:
			h, a = h-1, a+1
		}
	}
	return h, a
}

// adjusted is one side's inputs to the projection.
type adjusted struct {
	offense, defense, pace float64
}

func adjustedSide(side TeamStats) (adjusted, bool) {
	rating, ok := side.Ratings[models.RatingSourceAdjusted]
	if !ok || rating.Offense == nil || rating.Defense == nil || side.Season == nil || side.Season.Pace == nil {
		return adjusted{}, false
	}
	return adjusted{
		offense: rating.Offense.InexactFloat64(),
		defense: rating.Defense.InexactFloat64(),
		pace:    side.Season.Pace.InexactFloat64(),
	}, true
}

// winPercent is the chance, as a whole percentage, that a side projected to win by
// margin does win, assuming the real margin falls normally around the projection.
//
// Held to 1-99. A projection is not certain of anything, and "100%" on a page that
// is about choosing a bet reads as a promise.
func winPercent(margin float64) int {
	p := 0.5 * (1 + math.Erf(margin/(projectionMarginSD*math.Sqrt2)))
	return min(max(int(math.Round(p*100)), 1), 99)
}

// LineText is the posted line as "DUKE -4.5, O/U 151.5", with either half absent
// when no book has posted it, and empty when neither has. A spread of zero reads
// "pick'em".
//
// It states the line and nothing about it. The gap between the two numbers is the
// reader's to weigh; calling it an edge would claim more than one season's
// calibration can carry.
func (p *ScoreProjection) LineText(home, away models.Team) string {
	if p == nil {
		return ""
	}

	var parts []string
	if p.Spread != nil {
		switch {
		case p.Spread.HomeSpread.IsNegative():
			parts = append(parts, home.Abbreviation+" "+p.Spread.HomeSpread.String())
		case p.Spread.AwaySpread.IsNegative():
			parts = append(parts, away.Abbreviation+" "+p.Spread.AwaySpread.String())
		default:
			parts = append(parts, "pick'em")
		}
	}
	if p.OverUnder != nil {
		parts = append(parts, "O/U "+p.OverUnder.Total.String())
	}
	return strings.Join(parts, ", ")
}
