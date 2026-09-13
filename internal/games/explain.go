package games

// metricHelp is the hover text for a row of the matchup panel, keyed by the
// label the row is drawn with.
//
// It exists so the table can say a metric's name and nothing else. Every one of
// these numbers needs a sentence before it means anything -- SP+ and FPI are
// points, CORE is not, Elo knows only who won, win probability comes from the
// market rather than from any of them -- and a panel that prints all of those
// sentences is a panel nobody reads. The text is therefore allowed to be longer
// than a parenthetical could be: it costs nothing until it is asked for.
//
// Keyed by label rather than by a type of its own because the labels are
// already what the template ranges over, and a key that does not match a drawn
// row is a tooltip nobody ever sees -- which is why TestEveryDrawnRowIsExplained
// checks the two sets against each other.
var metricHelp = map[string]string{
	"Record": "Win-loss record for the season so far. Unlike the ratings below it, " +
		"this covers every division, so an FCS opponent has one.",

	"Away / Home": "The same record split by where the games were played: the " +
		"visitor's record on the road against the host's at home. Not shown for a " +
		"neutral-site game, where neither side is either.",

	"Against the spread": "How often each team has beaten the point spread this " +
		"season, which is not how often it has won. The figure in parentheses is the " +
		"average cover margin — points clear of the number, so a negative one means " +
		"the team has been falling short of what the market asked of it.",

	"SP+": "Bill Connelly's opponent- and tempo-adjusted efficiency rating, measured " +
		"in points: a team's rating is roughly how far it would be expected to beat a " +
		"perfectly average team by on a neutral field. The gap between two ratings is " +
		"a projected margin, which is where this row's projection comes from.",

	"FPI": "ESPN's Football Power Index: a forward-looking rating of how a team would " +
		"fare against an average opponent on a neutral field, blending how it has " +
		"played with returning production and recruiting.",

	"CORE": "CollegeFootballData's own opponent-adjusted efficiency rating. It is on a " +
		"scale of its own rather than in points, so it ranks teams against each other " +
		"and does not translate into a margin the way SP+ does.",

	"Elo": "A rating that moves only on results: the winner takes points from the " +
		"loser, more of them when the win was an upset. It knows who beat whom and not " +
		"how, which is what makes it a useful second opinion on a rating built from " +
		"play-by-play. This is the number going into this game rather than the team's " +
		"current one.",

	"Win probability": "The provider's pre-game win probability. It is derived from the " +
		"posted point spread rather than from any of the ratings above, so it follows " +
		"the market and exists only for games a sportsbook has priced.",

	"Recent": "The last five finished games of this season, newest first, with the " +
		"score from this team's point of view and the opponent beneath it. An @ marks a " +
		"road game. Week one has nothing to show.",
}

// Explain is the hover text for one row of the matchup panel.
//
// A rating source's entry ends with how to read the units nested under it,
// composed from the same RatingSummary the row used to carry in parentheses --
// so the scale is stated in one place and cannot drift from the rows it
// describes. It is appended only when those rows are actually drawn.
func (m *Matchup) Explain(label string) string {
	help := metricHelp[label]
	if m == nil || help == "" {
		return help
	}

	for source := range ratingUnitScales {
		if source.Label() != label {
			continue
		}
		if summary := m.RatingSummary(source); summary != "" {
			return help + " The unit figures beneath it are " + summary + "."
		}
	}
	return help
}
