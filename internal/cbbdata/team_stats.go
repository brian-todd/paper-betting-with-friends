package cbbdata

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/syncerr"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// SyncRatings stores this season's adjusted efficiency and SRS ratings.
func (s *SyncService) SyncRatings(ctx context.Context) error {
	return s.syncRatings(ctx, SeasonFor(s.clock.Now()))
}

// syncRatings stores one season's ratings, two requests for every team.
//
// The provider publishes nothing for a season's first days. An empty answer is not a
// failure, and only that season's rows are ever read for a game, so its pages show no
// rating rather than last season's. One table failing costs the other nothing.
func (s *SyncService) syncRatings(ctx context.Context, season int) error {
	return errors.Join(s.syncAdjustedRatings(ctx, season), s.syncSRSRatings(ctx, season))
}

func (s *SyncService) syncAdjustedRatings(ctx context.Context, season int) error {
	rows, err := s.client.GetAdjustedRatings(ctx, season)
	if err != nil {
		return fmt.Errorf("syncing adjusted ratings: %w", err)
	}

	externalIDs := make([]int64, len(rows))
	for i, row := range rows {
		externalIDs[i] = row.TeamID
	}
	teams, err := s.teamRepo.IDsByExternalID(models.SportBasketball, externalIDs)
	if err != nil {
		return fmt.Errorf("looking up rated teams: %w", err)
	}

	now := s.clock.Now()
	ratings := make([]models.TeamRating, 0, len(rows))
	for _, row := range rows {
		teamID, ok := teams[row.TeamID]
		if !ok || row.NetRating == nil {
			continue
		}
		ratings = append(ratings, models.TeamRating{
			TeamID:      teamID,
			Season:      season,
			Source:      models.RatingSourceAdjusted,
			FetchedAt:   now,
			Overall:     decimal.NewFromFloat(*row.NetRating),
			Offense:     decimalPtr(row.OffensiveRating),
			Defense:     decimalPtr(row.DefensiveRating),
			OverallRank: rankPtr(row.Rankings.Net),
			OffenseRank: rankPtr(row.Rankings.Offense),
			DefenseRank: rankPtr(row.Rankings.Defense),
		})
	}
	return s.storeRatings(models.RatingSourceAdjusted, len(rows), ratings)
}

func (s *SyncService) syncSRSRatings(ctx context.Context, season int) error {
	rows, err := s.client.GetSRSRatings(ctx, season)
	if err != nil {
		return fmt.Errorf("syncing srs ratings: %w", err)
	}

	externalIDs := make([]int64, len(rows))
	for i, row := range rows {
		externalIDs[i] = row.TeamID
	}
	teams, err := s.teamRepo.IDsByExternalID(models.SportBasketball, externalIDs)
	if err != nil {
		return fmt.Errorf("looking up rated teams: %w", err)
	}

	now := s.clock.Now()
	ratings := make([]models.TeamRating, 0, len(rows))
	for _, row := range rows {
		teamID, ok := teams[row.TeamID]
		if !ok || row.Rating == nil {
			continue
		}
		ratings = append(ratings, models.TeamRating{
			TeamID:    teamID,
			Season:    season,
			Source:    models.RatingSourceSRS,
			FetchedAt: now,
			Overall:   decimal.NewFromFloat(*row.Rating),
		})
	}
	return s.storeRatings(models.RatingSourceSRS, len(rows), ratings)
}

// storeRatings writes one source's rows. A response with rows that could place none
// of them is an error, as in syncRankings: the teams were never seeded, and the job
// would otherwise look healthy with no ratings on any page.
func (s *SyncService) storeRatings(source models.RatingSource, published int, ratings []models.TeamRating) error {
	if published > 0 && len(ratings) == 0 {
		return fmt.Errorf("storing %s ratings: none of the %d rated teams are stored, so basketball teams need seeding", source, published)
	}

	var failed syncerr.Tally
	for i := range ratings {
		if err := s.teamRatingRepo.Upsert(&ratings[i]); err != nil {
			s.logger.Error("failed to store rating", "source", source, "team", ratings[i].TeamID, "error", err)
			failed.Add(err)
		}
	}
	s.logger.Info("synced ratings", "source", source, "published", published, "stored", len(ratings)-failed.Count())
	return failed.Err(string(source) + " ratings")
}

// SyncTeamStats stores this season's team figures and records.
func (s *SyncService) SyncTeamStats(ctx context.Context) error {
	return s.syncTeamStats(ctx, SeasonFor(s.clock.Now()))
}

// syncTeamStats stores one season's figures and records, one request for every team.
//
// The feed's overall record is kept beside the home and away split this works out
// from the stored games, because a team's games may not all be stored. A team outside
// Division I appears only for the games it played against one, so its record is that
// and no more.
func (s *SyncService) syncTeamStats(ctx context.Context, season int) error {
	rows, err := s.client.GetTeamSeasonStats(ctx, season)
	if err != nil {
		return fmt.Errorf("syncing team stats: %w", err)
	}

	externalIDs := make([]int64, len(rows))
	for i, row := range rows {
		externalIDs[i] = row.TeamID
	}
	teams, err := s.teamRepo.IDsByExternalID(models.SportBasketball, externalIDs)
	if err != nil {
		return fmt.Errorf("looking up teams for stats: %w", err)
	}
	if len(rows) > 0 && len(teams) == 0 {
		return fmt.Errorf("storing team stats: none of the %d teams are stored, so basketball teams need seeding", len(rows))
	}

	splits, err := s.teamRecordRepo.BasketballSplits(season)
	if err != nil {
		return fmt.Errorf("reading home and away results: %w", err)
	}

	now := s.clock.Now()
	var failed syncerr.Tally
	for _, row := range rows {
		teamID, ok := teams[row.TeamID]
		if !ok {
			continue
		}
		if err := s.teamStatsRepo.Upsert(teamSeasonStatsFrom(teamID, season, row, now)); err != nil {
			s.logger.Error("failed to store team stats", "team", row.TeamID, "error", err)
			failed.Add(err)
		}
		if err := s.teamRecordRepo.Upsert(teamRecordFrom(teamID, season, row, splits[teamID], now)); err != nil {
			s.logger.Error("failed to store team record", "team", row.TeamID, "error", err)
			failed.Add(err)
		}
	}
	s.logger.Info("synced team stats", "teams", len(rows), "failed", failed.Count())
	return failed.Err("team stats")
}

func teamSeasonStatsFrom(teamID uuid.UUID, season int, row APITeamSeasonStats, now time.Time) *models.TeamSeasonStats {
	team, opponent := row.TeamStats, row.OpponentStats
	return &models.TeamSeasonStats{
		TeamID:                teamID,
		Season:                season,
		FetchedAt:             now,
		Games:                 int(row.Games),
		Pace:                  decimalPtr(row.Pace),
		OffenseRating:         decimalPtr(team.Rating),
		DefenseRating:         decimalPtr(opponent.Rating),
		OffenseEffectiveFGPct: decimalPtr(team.FourFactors.EffectiveFGPct),
		DefenseEffectiveFGPct: decimalPtr(opponent.FourFactors.EffectiveFGPct),
		OffenseTurnoverPct:    decimalPtr(team.TurnoverPct()),
		DefenseTurnoverPct:    decimalPtr(opponent.TurnoverPct()),
		OffenseReboundPct:     decimalPtr(team.FourFactors.OffensiveReboundPct),
		DefenseReboundPct:     decimalPtr(opponent.FourFactors.OffensiveReboundPct),
		OffenseFreeThrowRate:  decimalPtr(team.FourFactors.FreeThrowRate),
		DefenseFreeThrowRate:  decimalPtr(opponent.FourFactors.FreeThrowRate),
		OffenseThreePct:       decimalPtr(team.ThreePoint.Pct),
		DefenseThreePct:       decimalPtr(opponent.ThreePoint.Pct),
	}
}

func teamRecordFrom(teamID uuid.UUID, season int, row APITeamSeasonStats, split repository.TeamSplit, now time.Time) *models.TeamRecord {
	return &models.TeamRecord{
		TeamID:     teamID,
		Season:     season,
		FetchedAt:  now,
		Games:      int(row.Games),
		Wins:       int(row.Wins),
		Losses:     int(row.Losses),
		HomeGames:  split.HomeWins + split.HomeLosses,
		HomeWins:   split.HomeWins,
		HomeLosses: split.HomeLosses,
		AwayGames:  split.AwayWins + split.AwayLosses,
		AwayWins:   split.AwayWins,
		AwayLosses: split.AwayLosses,
	}
}

// RatingsMissing reports whether a process starting at now should fetch ratings rather
// than wait for the daily slot: it is in season and this season has none stored. Like
// RankingsMissing, that spends a run per restart until the provider first publishes.
func (s *SyncService) RatingsMissing(now time.Time, loc *time.Location) bool {
	if !InSeason(now, loc) {
		return false
	}
	found, err := s.teamRatingRepo.HasSeason(SeasonFor(now), models.RatingSourceAdjusted)
	if err != nil {
		s.logger.Error("failed to check for stored ratings", "error", err)
		return false
	}
	return !found
}

// TeamStatsMissing reports whether a process starting at now should fetch team stats
// rather than wait for the next slot.
func (s *SyncService) TeamStatsMissing(now time.Time, loc *time.Location) bool {
	if !InSeason(now, loc) {
		return false
	}
	found, err := s.teamStatsRepo.HasSeason(SeasonFor(now))
	if err != nil {
		s.logger.Error("failed to check for stored team stats", "error", err)
		return false
	}
	return !found
}

func decimalPtr(v *float64) *decimal.Decimal {
	if v == nil {
		return nil
	}
	d := decimal.NewFromFloat(*v)
	return &d
}

func rankPtr(v *float64) *int {
	if v == nil {
		return nil
	}
	rank := int(math.Round(*v))
	return &rank
}
