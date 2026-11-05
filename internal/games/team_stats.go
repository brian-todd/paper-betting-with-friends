package games

import (
	"fmt"
	"log/slog"
	"strconv"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/shopspring/decimal"
)

// TeamStatsPanel is basketball's table of team numbers. The rows are the same either
// way, so a team's night reads directly against its season.
type TeamStatsPanel struct {
	// InThisGame says the figures are this game's box score, not season averages.
	InThisGame bool
	Rows       []TeamStatsRow
	Note       string
}

// TeamStatsRow is one line of the panel, already formatted.
type TeamStatsRow struct {
	Label      string
	Away, Home string
}

// teamStatsPanel builds the panel for a game, or nil when there is nothing to show. A
// finished game shows its box score once there is one that agrees with the final score;
// everything else, a finished game still waiting for one included, shows the season. A
// failed lookup costs the box score and nothing else.
func (s *Service) teamStatsPanel(game *models.Game, result *models.GameResult, matchup *Matchup) *TeamStatsPanel {
	if game.Status == models.GameStatusFinal && result.IsFinal() {
		boxes, err := s.gameTeamStatsRepo.FindForGame(game.ID)
		if err != nil {
			slog.Error("failed to fetch box score", "game", game.ID, "error", err)
		} else if away, home := boxes[game.AwayTeamID].Box, boxes[game.HomeTeamID].Box; boxAgrees(away, result.AwayScore) && boxAgrees(home, result.HomeScore) {
			return &TeamStatsPanel{InThisGame: true, Rows: teamStatsRows(away, home, totalText, totalText)}
		}
	}

	if matchup == nil {
		return nil
	}
	away, home := matchup.Away.Season, matchup.Home.Season
	if !away.HasGames() || !home.HasGames() || !away.Box.Reported() || !home.Box.Reported() {
		return nil
	}
	return &TeamStatsPanel{
		Rows: teamStatsRows(away.Box, home.Box, perGameText(away.Games), perGameText(home.Games)),
		Note: "Based on " + gamesText(away.Games) + " for " + game.AwayTeam.Abbreviation +
			" and " + gamesText(home.Games) + " for " + game.HomeTeam.Abbreviation + " this season.",
	}
}

// boxAgrees reports whether a box score adds up to the team's final score. One that does
// not was read while the game was being played or before a correction, and the next
// sync can replace it.
func boxAgrees(box models.TeamBox, score int) bool {
	return box.Reported() && *box.Points == score
}

// teamStatsRows is the panel's rows, in order. Each side has its own formatter because
// each side's averages have their own number of games. The shooting percentages come
// from the counts, so they are the same whichever formatter is in use.
func teamStatsRows(away, home models.TeamBox, awayCount, homeCount func(*int) string) []TeamStatsRow {
	count := func(label string, read func(models.TeamBox) *int) TeamStatsRow {
		return TeamStatsRow{Label: label, Away: awayCount(read(away)), Home: homeCount(read(home))}
	}
	shots := func(label string, made, attempted func(models.TeamBox) *int) []TeamStatsRow {
		return []TeamStatsRow{
			{
				Label: label,
				Away:  madeAttemptedText(awayCount, made(away), attempted(away)),
				Home:  madeAttemptedText(homeCount, made(home), attempted(home)),
			},
			{
				Label: label + " %",
				Away:  shootingPctText(made(away), attempted(away)),
				Home:  shootingPctText(made(home), attempted(home)),
			},
		}
	}

	rows := []TeamStatsRow{count("Points", func(b models.TeamBox) *int { return b.Points })}
	rows = append(rows, shots("Field goals", func(b models.TeamBox) *int { return b.FGMade }, func(b models.TeamBox) *int { return b.FGAttempted })...)
	rows = append(rows, shots("3-pointers", func(b models.TeamBox) *int { return b.ThreeMade }, func(b models.TeamBox) *int { return b.ThreeAttempted })...)
	rows = append(rows, shots("Free throws", func(b models.TeamBox) *int { return b.FTMade }, func(b models.TeamBox) *int { return b.FTAttempted })...)
	return append(rows,
		count("Rebounds", func(b models.TeamBox) *int { return b.Rebounds }),
		count("Offensive rebounds", func(b models.TeamBox) *int { return b.OffRebounds }),
		count("Defensive rebounds", func(b models.TeamBox) *int { return b.DefRebounds }),
		count("Assists", func(b models.TeamBox) *int { return b.Assists }),
		count("Steals", func(b models.TeamBox) *int { return b.Steals }),
		count("Blocks", func(b models.TeamBox) *int { return b.Blocks }),
		count("Turnovers", func(b models.TeamBox) *int { return b.Turnovers }),
		count("Fouls", func(b models.TeamBox) *int { return b.Fouls }),
		count("Points in the paint", func(b models.TeamBox) *int { return b.PaintPoints }),
		count("Fast-break points", func(b models.TeamBox) *int { return b.FastBreakPoints }),
		count("Points off turnovers", func(b models.TeamBox) *int { return b.PointsOffTurnovers }),
	)
}

// totalText renders a count as it stands, empty when it is missing.
func totalText(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// perGameText renders a season's count as an average over its games, to one decimal
// place. Callers have already checked there are games.
func perGameText(games int) func(*int) string {
	return func(v *int) string {
		if v == nil || games <= 0 {
			return ""
		}
		return decimal.NewFromInt(int64(*v)).Div(decimal.NewFromInt(int64(games))).StringFixed(1)
	}
}

func madeAttemptedText(count func(*int) string, made, attempted *int) string {
	if made == nil || attempted == nil {
		return ""
	}
	return fmt.Sprintf("%s-%s", count(made), count(attempted))
}

func shootingPctText(made, attempted *int) string {
	if made == nil || attempted == nil || *attempted <= 0 {
		return ""
	}
	pct := decimal.NewFromInt(int64(*made)).Mul(decimal.NewFromInt(100)).Div(decimal.NewFromInt(int64(*attempted)))
	return percentText(&pct)
}
