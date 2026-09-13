package games

import (
	"strconv"

	"github.com/shopspring/decimal"
)

// EfficiencyRow is one unit's line under a metric: which side of the ball it
// is, and both teams' already-formatted values.
//
// Formatted here rather than in the template because the units differ per
// metric -- a success rate is a percentage, points per opportunity is points,
// PPA is a bare number -- and a template that had to choose between them per
// row would be a switch statement written in a language that does not have one.
type EfficiencyRow struct {
	Label      string
	Away, Home string
}

// EfficiencyMetric is one metric with both units nested under it.
//
// Metric-major rather than unit-major, because that is the shape the data is
// in: every figure /stats/season/advanced publishes comes in an offensive and a
// defensive flavour, and putting the pair adjacent is what makes a defence's
// 0.31 legible next to an offence's 0.24.
type EfficiencyMetric struct {
	Label string

	// Help is the metric's hover text: what the number measures and which way
	// each unit wants it.
	//
	// It sits on the metric rather than on the two rows beneath it because the
	// direction is a property of the pair: an offense wants more of everything
	// here and a defense fewer -- except havoc, which reverses, and reads as
	// the exception it is precisely because the other five do not.
	//
	// Hover text rather than a caption for the same reason the matchup panel
	// moved its own to a title -- see metricHelp. A definition costs nothing
	// until it is asked for, which is what lets it be a definition rather than
	// a hint.
	Help string

	// Units are the offensive and defensive figures, in that order.
	Units []EfficiencyRow
}

// HasEfficiency reports whether both sides have season efficiency worth
// drawing.
//
// Both, for the same reason HasRating demands both: /stats/season/advanced is
// FBS-only, and half a panel invites a conclusion about the missing team.
// Reliability is part of the test -- a team 40 plays into a season has numbers
// that describe a quarter of football, and showing them next to an opponent's
// full season is a comparison of two different things.
func (m *Matchup) HasEfficiency() bool {
	if m == nil {
		return false
	}
	return m.Home.Advanced.OffenseReliable() && m.Away.Advanced.OffenseReliable() &&
		m.Home.Advanced.DefenseReliable() && m.Away.Advanced.DefenseReliable()
}

// EfficiencyMetrics is the whole panel: six metrics, each with its offensive
// and defensive line beneath it.
func (m *Matchup) EfficiencyMetrics() []EfficiencyMetric {
	if !m.HasEfficiency() {
		return nil
	}

	home, away := m.Home.Advanced, m.Away.Advanced

	// Both units of a metric are the same measurement taken against opposite
	// ends of the ball, so they always share a formatter.
	unit := func(label string, format func(*decimal.Decimal) string, awayValue, homeValue *decimal.Decimal) EfficiencyRow {
		return EfficiencyRow{Label: label, Away: format(awayValue), Home: format(homeValue)}
	}

	const offenseFirst = "more for an offense, fewer for a defense"

	return []EfficiencyMetric{
		{
			Label: "PPA / play",
			Help: "Predicted points added per snap: how much each play moved the " +
				"expected points of the drive it belonged to; " + offenseFirst + ".",
			Units: []EfficiencyRow{
				unit("Offense", ppaText, away.OffensePPA, home.OffensePPA),
				unit("Defense", ppaText, away.DefensePPA, home.DefensePPA),
			},
		},
		{
			Label: "Success rate",
			Help: "The share of plays that gained enough to stay on schedule — half " +
				"the yards to go on first down, seventy per cent on second, all of them " +
				"on third or fourth; " + offenseFirst + ".",
			Units: []EfficiencyRow{
				unit("Offense", rateText, away.OffenseSuccessRate, home.OffenseSuccessRate),
				unit("Defense", rateText, away.DefenseSuccessRate, home.DefenseSuccessRate),
			},
		},
		{
			Label: "Explosiveness",
			Help: "The average points added by the plays that succeeded: how much a " +
				"team gains when it gains at all, as opposed to how often; " + offenseFirst + ".",
			Units: []EfficiencyRow{
				unit("Offense", perPlayText, away.OffenseExplosiveness, home.OffenseExplosiveness),
				unit("Defense", perPlayText, away.DefenseExplosiveness, home.DefenseExplosiveness),
			},
		},
		{
			Label: "Line yards",
			Help: "The share of a run's yardage credited to the line rather than to " +
				"the back, under the standard line-yards formula; " + offenseFirst + ".",
			Units: []EfficiencyRow{
				unit("Offense", perPlayText, away.OffenseLineYards, home.OffenseLineYards),
				unit("Defense", perPlayText, away.DefenseLineYards, home.DefenseLineYards),
			},
		},
		{
			Label: "Points / opportunity",
			Help: "Points scored per drive that reached the opponent's forty-yard " +
				"line: how well a team finishes once it has moved the ball; " + offenseFirst + ".",
			Units: []EfficiencyRow{
				unit("Offense", perPlayText, away.OffensePointsPerOpportunity, home.OffensePointsPerOpportunity),
				unit("Defense", perPlayText, away.DefensePointsPerOpportunity, home.DefensePointsPerOpportunity),
			},
		},
		{
			// The one metric that runs the other way, which is why every note
			// above it says which way it runs.
			Label: "Havoc",
			Help: "The share of plays a defense ended behind the line or at the " +
				"ball — tackles for loss, sacks, forced fumbles, interceptions and pass " +
				"breakups; more for a defense, fewer for an offense.",
			Units: []EfficiencyRow{
				unit("Offense", rateText, away.OffenseHavoc, home.OffenseHavoc),
				unit("Defense", rateText, away.DefenseHavoc, home.DefenseHavoc),
			},
		},
	}
}

// EfficiencySample describes how much football each side's efficiency figures
// rest on, for the panel's footnote. Empty when there is nothing to draw.
//
// This is the disclosure the sample-size gate deliberately does not make: two
// teams one game in produce a panel that looks exactly as authoritative as two
// teams ten games in, and the only honest fix is to say how much football is
// behind it. Each side is named, because "84 and 77" leaves the reader to guess
// which column is which.
func (m *Matchup) EfficiencySample() string {
	if !m.HasEfficiency() {
		return ""
	}
	return "Based on " +
		plays(m.Away.Advanced.OffensePlays) + " offensive plays for " + m.Away.Team.Abbreviation +
		" and " +
		plays(m.Home.Advanced.OffensePlays) + " for " + m.Home.Team.Abbreviation + " this season."
}

func plays(n *int) string {
	if n == nil {
		return "0"
	}
	return strconv.Itoa(*n)
}

// efficiencyPercentScale turns a 0-1 rate into a percentage.
var efficiencyPercentScale = decimal.NewFromInt(100)

// The formatters below all use StringFixed rather than Round, which is the
// difference between a column and a list. String drops trailing zeros, so a
// points-per-opportunity of exactly 6 renders as "6" directly above a "4.43" --
// and a table whose whole purpose is putting two numbers side by side should
// not make the reader align the decimal points themselves. The CSS asks for
// tabular figures for the same reason.

// rateText renders a 0-1 rate as a percentage to one decimal place.
func rateText(v *decimal.Decimal) string {
	if v == nil {
		return ""
	}
	return v.Mul(efficiencyPercentScale).StringFixed(1) + "%"
}

// ppaText renders points added per play, which is small and signed, to two
// decimal places. The sign is the whole point of the number and is never
// dropped: a positive PPA is a good offence and a bad defence.
func ppaText(v *decimal.Decimal) string {
	if v == nil {
		return ""
	}

	if v.IsNegative() {
		return v.StringFixed(2)
	}
	return "+" + v.StringFixed(2)
}

// perPlayText renders an unsigned per-play figure to two decimal places.
func perPlayText(v *decimal.Decimal) string {
	if v == nil {
		return ""
	}
	return v.StringFixed(2)
}

// HasATS reports whether both sides have an against-the-spread record with
// graded games behind it.
func (m *Matchup) HasATS() bool {
	if m == nil {
		return false
	}
	return m.Home.ATS.Record().Played() && m.Away.ATS.Record().Played()
}
