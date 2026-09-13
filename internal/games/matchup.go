package games

import (
	"strconv"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// HomeFieldPoints is the margin conventionally credited to the home side when
// turning a difference of two neutral-site ratings into a projected result.
//
// It is a convention rather than a number any of the rating providers publish,
// which is why the page attributes the projection to SP+ and the adjustment to
// itself. Not applied to a neutral-site game.
var HomeFieldPoints = decimal.RequireFromString("2.5")

// RecentResult is one finished game as the form strip shows it: what the team
// did, to whom, and by what score.
type RecentResult struct {
	Game models.Game

	// Won is nil for a tie, which college football technically still allows.
	Won *bool

	// For and Against are this team's points and the opponent's, so the strip
	// does not have to know which side of the game the team was on.
	For     int
	Against int

	Opponent models.Team

	// AtHome distinguishes "beat Ohio State" from "beat Ohio State on the road",
	// which is most of what a result means.
	AtHome bool
}

// Outcome is "W", "L" or "T", for the strip's badge.
func (r RecentResult) Outcome() string {
	switch {
	case r.Won == nil:
		return "T"
	case *r.Won:
		return "W"
	default:
		return "L"
	}
}

// Score renders the result from this team's perspective, e.g. "31-17".
func (r RecentResult) Score() string {
	return strconv.Itoa(r.For) + "-" + strconv.Itoa(r.Against)
}

// TeamStats is everything the comparison panel knows about one side.
type TeamStats struct {
	Team    models.Team
	Record  *models.TeamRecord
	Ratings map[models.RatingSource]models.TeamRating
	Form    []RecentResult

	// ATS is the record against the spread. Coverage is wider than the
	// ratings -- it is every team anyone has posted a line on -- so an FCS side
	// usually has one.
	ATS *models.TeamATSRecord

	// Advanced is season efficiency, FBS-only like the ratings.
	Advanced *models.TeamAdvancedStats

	// PregameElo is the /games feed's Elo going into this game. Unlike the rest
	// of this struct it is per-game rather than per-season, so it is as of
	// kickoff rather than as of the last sync.
	PregameElo *int
}

// Rating returns one source's rating and whether the team has one. FBS-only
// coverage means an FCS opponent has records and form but no ratings at all.
func (t TeamStats) Rating(source models.RatingSource) (models.TeamRating, bool) {
	rating, ok := t.Ratings[source]
	return rating, ok
}

// Matchup is the comparison panel: both sides, plus the one number derived from
// them.
type Matchup struct {
	Home TeamStats
	Away TeamStats

	// NeutralSite mirrors the game, so the template does not have to reach back
	// through it to explain why no home-field adjustment was applied.
	NeutralSite bool

	// WinProbability is the provider's pre-game number for this game. Nil for
	// most games: it is derived from a posted line, so its coverage follows the
	// market rather than the schedule.
	WinProbability *models.GamePregameWinProbability
}

// HasAny reports whether there is anything at all to draw. A game whose teams
// have never been synced renders no panel rather than an empty one.
func (m *Matchup) HasAny() bool {
	if m == nil {
		return false
	}
	if m.WinProbability != nil {
		return true
	}
	for _, side := range []TeamStats{m.Home, m.Away} {
		if side.Record != nil || len(side.Ratings) > 0 || len(side.Form) > 0 || side.PregameElo != nil {
			return true
		}
		if side.ATS != nil || side.Advanced != nil {
			return true
		}
	}
	return false
}

// HasRating reports whether *both* sides have a rating from this source.
//
// Both, not either: a row reading "— | 14.2" invites the reader to conclude the
// unrated team is bad rather than uncovered, and an FBS side hosting an FCS one
// is a normal September Saturday. The template drops the whole row instead.
func (m *Matchup) HasRating(source models.RatingSource) bool {
	if m == nil {
		return false
	}
	_, home := m.Home.Ratings[source]
	_, away := m.Away.Ratings[source]
	return home && away
}

// RatingRows lists the sources both sides are rated by, in display order, so
// the template ranges over one slice instead of repeating itself per source.
func (m *Matchup) RatingRows() []models.RatingSource {
	if m == nil {
		return nil
	}

	var rows []models.RatingSource
	for _, source := range []models.RatingSource{models.RatingSourceSP, models.RatingSourceFPI, models.RatingSourceCORE} {
		if m.HasRating(source) {
			rows = append(rows, source)
		}
	}
	return rows
}

// HasElo reports whether both sides carry a pregame Elo.
func (m *Matchup) HasElo() bool {
	return m != nil && m.Home.PregameElo != nil && m.Away.PregameElo != nil
}

// RatingsAreFBSOnly reports whether the panel dropped rating rows because one
// side is outside the divisions the ratings cover. It drives the footnote that
// explains a half-height panel, and is deliberately narrow: it is true only when
// one side has ratings and the other has none, which is the case the footnote
// describes. Two unrated teams is a sync that has not run, and says nothing.
func (m *Matchup) RatingsAreFBSOnly() bool {
	if m == nil {
		return false
	}
	return (len(m.Home.Ratings) > 0) != (len(m.Away.Ratings) > 0)
}

// AsOf is the most recent fetch behind anything in the panel, for the "as of"
// line. Nil when the panel is drawn from per-game data alone -- pregame Elo
// arrives with the game and needs no such caveat.
//
// The newest rather than the oldest: the line exists to tell a reader how stale
// the panel might be, and claiming it is older than it is would be its own kind
// of wrong.
func (m *Matchup) AsOf() *time.Time {
	if m == nil {
		return nil
	}

	var newest time.Time
	consider := func(t time.Time) {
		if t.After(newest) {
			newest = t
		}
	}

	for _, side := range []TeamStats{m.Home, m.Away} {
		for _, rating := range side.Ratings {
			consider(rating.FetchedAt)
		}
		if side.Record != nil {
			consider(side.Record.FetchedAt)
		}
		if side.ATS != nil {
			consider(side.ATS.FetchedAt)
		}
		if side.Advanced != nil {
			consider(side.Advanced.FetchedAt)
		}
	}
	if m.WinProbability != nil {
		consider(m.WinProbability.FetchedAt)
	}

	if newest.IsZero() {
		return nil
	}
	return &newest
}

// SPProjection is SP+'s projected margin for this game.
//
// The difference of two SP+ ratings is that model's projected neutral-site
// margin; HomeFieldPoints turns it into a projection for this venue. Nil unless
// both sides are rated, because there is nothing to difference otherwise and a
// division-average stand-in would be inventing the input.
func (m *Matchup) SPProjection() *Projection {
	if m == nil || !m.HasRating(models.RatingSourceSP) {
		return nil
	}

	home := m.Home.Ratings[models.RatingSourceSP].Overall
	away := m.Away.Ratings[models.RatingSourceSP].Overall

	margin := home.Sub(away)
	if !m.NeutralSite {
		margin = margin.Add(HomeFieldPoints)
	}

	projection := &Projection{Margin: margin.Abs().Round(1), Favorite: m.Home.Team}
	if margin.IsNegative() {
		projection.Favorite = m.Away.Team
	}
	return projection
}

// Projection is a model's expected result, as a favourite and a margin. Kept as
// those two rather than a signed number so the template never has to decide what
// the sign meant.
type Projection struct {
	Favorite models.Team
	Margin   decimal.Decimal
}

// recentResultFrom turns a finished game into the form strip's view of it from
// teamID's perspective. The second return is false for a game with no final
// result, which the query already excludes.
func recentResultFrom(game models.Game, teamID uuid.UUID) (RecentResult, bool) {
	if !game.Result.IsFinal() {
		return RecentResult{}, false
	}

	result := RecentResult{Game: game, AtHome: game.HomeTeamID == teamID}
	if result.AtHome {
		result.For, result.Against = game.Result.HomeScore, game.Result.AwayScore
		result.Opponent = game.AwayTeam
	} else {
		result.For, result.Against = game.Result.AwayScore, game.Result.HomeScore
		result.Opponent = game.HomeTeam
	}

	if result.For != result.Against {
		won := result.For > result.Against
		result.Won = &won
	}
	return result, true
}

// RatingUnit is one unit line beneath a rating row -- offense, defense or
// special teams -- with both sides' values and their ranks where the source
// publishes them.
//
// No note of its own: which way each unit runs is said once, on the row above,
// the way the efficiency panel says it once per metric. Three sub-rows each
// repeating half of one sentence is three ways to read the same rule.
type RatingUnit struct {
	Label              string
	Away, Home         string
	AwayRank, HomeRank *int
}

// ratingUnitScale says how one source's unit ratings read.
//
// A source absent from this table draws no units at all, whatever the provider
// sends, and one whose specialTeams is false draws no special teams row. The
// summary is this page's claim about what the numbers mean, and a column whose
// direction nobody can state is worse than no column.
type ratingUnitScale struct {
	// summary is the single line the rating row carries: what its units are
	// measured in and which way each of them runs.
	summary string

	// specialTeams is whether the source publishes one. CORE does not, and if
	// it began to tomorrow there would still be nothing true to say about it.
	specialTeams bool
}

// ratingUnitScales was checked against the stored ratings rather than assumed.
//
// Across the 138 rated teams of the current season, a team's overall rating
// correlates +0.94 with its SP+ offense and -0.94 with its SP+ defense,
// +0.91/-0.91 for CORE, and +0.63/+0.61 for FPI. SP+ is the control: its
// convention is documented, and the method reproduces it.
//
// So CORE scores its defense the way SP+ scores its own -- lower is better, and
// overall is literally offense minus defense, exactly (Ohio State 19.2 = 8.4 -
// -10.8) -- while FPI's are percentiles that run the same way for both units.
// The two defensive scales still differ in sign: an elite SP+ defense is a small
// positive number of points allowed, an elite CORE defense a large negative one.
// Nothing may difference or average across sources.
var ratingUnitScales = map[models.RatingSource]ratingUnitScale{
	models.RatingSourceSP: {
		summary:      "points against an average opponent; offense high, defense low",
		specialTeams: true,
	},
	models.RatingSourceFPI: {
		summary:      "percentiles; higher is better throughout",
		specialTeams: true,
	},
	models.RatingSourceCORE: {
		summary: "opponent-relative efficiency; offense high, defense low",
	},
}

// RatingSummary is the line a rating row carries describing the units nested
// under it.
//
// Empty when none are drawn, because there is then nothing for it to describe --
// a caption explaining how to read rows that are not on the page is worse than
// no caption.
func (m *Matchup) RatingSummary(source models.RatingSource) string {
	if len(m.RatingUnits(source)) == 0 {
		return ""
	}
	return ratingUnitScales[source].summary
}

// RatingUnits breaks one rating row into the unit ratings behind it.
//
// Each unit applies the both-sides-or-neither rule of HasRating independently
// of the others, for the same reason: the nested objects are sparsely populated
// and a side can have a special teams rating with no offense rating at all.
//
// Ranks come through where the source publishes them, which today is SP+ alone.
func (m *Matchup) RatingUnits(source models.RatingSource) []RatingUnit {
	if m == nil || !m.HasRating(source) {
		return nil
	}
	scale, ok := ratingUnitScales[source]
	if !ok {
		return nil
	}

	home := m.Home.Ratings[source]
	away := m.Away.Ratings[source]

	candidates := []RatingUnit{
		{
			Label:    "Offense",
			Away:     unitText(away.Offense),
			Home:     unitText(home.Offense),
			AwayRank: away.OffenseRank,
			HomeRank: home.OffenseRank,
		},
		{
			Label:    "Defense",
			Away:     unitText(away.Defense),
			Home:     unitText(home.Defense),
			AwayRank: away.DefenseRank,
			HomeRank: home.DefenseRank,
		},
	}
	if scale.specialTeams {
		candidates = append(candidates, RatingUnit{
			// No ranks: none of the three publishes a special teams ranking.
			Label: "Special teams",
			Away:  unitText(away.SpecialTeams),
			Home:  unitText(home.SpecialTeams),
		})
	}

	var units []RatingUnit
	for _, unit := range candidates {
		if unit.Away != "" && unit.Home != "" {
			units = append(units, unit)
		}
	}
	return units
}

// unitText renders a unit rating the way OverallText renders the headline it
// sits beneath -- one decimal place, trailing zeroes dropped -- so the whole
// SP+ block reads on one convention.
//
// Empty for a unit the source did not publish, which is how SPUnits tells a
// missing unit from a zero one. A special teams rating of exactly 0.0 is a
// perfectly ordinary team.
func unitText(v *decimal.Decimal) string {
	if v == nil {
		return ""
	}
	return v.Round(1).String()
}

// OpponentText names who a result came against, and where. "@" for a road game
// and "vs" for a home one, because "beat Ohio State" and "beat Ohio State in
// Columbus" are not the same claim about a team.
func (r RecentResult) OpponentText() string {
	if r.AtHome {
		return "vs " + r.Opponent.Abbreviation
	}
	return "@ " + r.Opponent.Abbreviation
}

// FormRow is one line of the recent-form breakout: the same distance back for
// both sides.
//
// The two sides are paired by *count*, not by date -- a team coming off a bye
// played its last game a week earlier than the other side did. Newest first,
// and unlabelled: the rows are in order, and five rows each saying how far back
// they are is five rows of counting the reader can already do.
//
// Either side may be nil. A team two games into the season sits opposite one
// five games in whenever a schedule opens with byes or a cancellation, and the
// short side draws a dash rather than shortening the whole breakout.
type FormRow struct {
	Away, Home *RecentResult
}

// FormRows breaks recent form out into a line per game, newest first.
//
// Unlike the rating rows this does not demand both sides: a missing result here
// reads as "they have not played that many games", which is exactly what it
// means. There is no misleading comparison available for it to invite.
func (m *Matchup) FormRows() []FormRow {
	if m == nil {
		return nil
	}

	count := max(len(m.Away.Form), len(m.Home.Form))
	rows := make([]FormRow, 0, count)
	for i := range count {
		var row FormRow
		if i < len(m.Away.Form) {
			row.Away = &m.Away.Form[i]
		}
		if i < len(m.Home.Form) {
			row.Home = &m.Home.Form[i]
		}
		rows = append(rows, row)
	}
	return rows
}
