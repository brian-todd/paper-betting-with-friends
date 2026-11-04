package games

import (
	"fmt"
	"log/slog"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
)

const (
	// lineupUnitsShown is how many units the panel lists for each team.
	lineupUnitsShown = 5

	// minPossessionsForNet is the fewest possessions a side needs for the panel to
	// show a net rating. Under it, one made three moves the number by tens of points.
	minPossessionsForNet = 10
)

// LineupPanel is the game page's five-man unit panel.
type LineupPanel struct {
	// InThisGame says the units are from this game, rather than the season before it.
	InThisGame bool
	Sides      []LineupSide
}

// LineupSide is one team's units.
type LineupSide struct {
	Team  string
	Units []LineupUnit
}

// LineupUnit is one row of the panel.
type LineupUnit struct {
	Players     string
	Minutes     int
	Possessions int
	// Net is empty when the sample is too small to mean anything.
	Net string
}

// lineupPanel builds the panel for a game, or nil when there is nothing to show.
//
// A finished game shows its own units once the feed has them. Everything else
// shows the season before tip-off, which is also what a finished game falls back
// to, so a page never credits a team with what came after the game. A failed lookup
// costs the panel and nothing else.
func (s *Service) lineupPanel(game *models.Game) *LineupPanel {
	if game.Status == models.GameStatusFinal {
		sides, err := s.lineupSides(game, func(teamID uuid.UUID) ([]models.LineupTotals, error) {
			return s.gameLineupRepo.TopForGame(game.ID, teamID, lineupUnitsShown)
		})
		if err != nil {
			slog.Error("failed to fetch game lineups", "game", game.ID, "error", err)
			return nil
		}
		if len(sides) > 0 {
			return &LineupPanel{InThisGame: true, Sides: sides}
		}
	}

	sides, err := s.lineupSides(game, func(teamID uuid.UUID) ([]models.LineupTotals, error) {
		return s.gameLineupRepo.TopForSeason(teamID, game.Season, game.ScheduledAt, lineupUnitsShown)
	})
	if err != nil {
		slog.Error("failed to fetch season lineups", "game", game.ID, "error", err)
		return nil
	}
	if len(sides) == 0 {
		return nil
	}
	return &LineupPanel{Sides: sides}
}

// lineupSides reads both teams, away first as the page does, and leaves out a team
// with no units: the feed often has no data for a non-Division I opponent.
func (s *Service) lineupSides(game *models.Game, read func(uuid.UUID) ([]models.LineupTotals, error)) ([]LineupSide, error) {
	var sides []LineupSide
	for _, team := range []models.Team{game.AwayTeam, game.HomeTeam} {
		totals, err := read(team.ID)
		if err != nil {
			return nil, err
		}
		if len(totals) == 0 {
			continue
		}

		units := make([]LineupUnit, len(totals))
		for i, unit := range totals {
			units[i] = lineupUnit(unit)
		}
		sides = append(sides, LineupSide{Team: team.Name, Units: units})
	}
	return sides, nil
}

func lineupUnit(totals models.LineupTotals) LineupUnit {
	unit := LineupUnit{
		Players:     totals.Players(),
		Minutes:     (totals.Seconds + 30) / 60,
		Possessions: totals.PossessionsFor,
	}
	if net, ok := totals.NetRating(); ok &&
		totals.PossessionsFor >= minPossessionsForNet && totals.PossessionsAgainst >= minPossessionsForNet {
		unit.Net = fmt.Sprintf("%+.1f", net)
	}
	return unit
}
