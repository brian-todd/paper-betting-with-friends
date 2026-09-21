package cfbdata

import (
	"context"
	"errors"
	"fmt"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// errNoRows reports an upstream response that resolved to nothing writable.
//
// It is an error rather than a quiet return because these rows are upserted in
// place: a truncated response would overwrite every team's rating with nothing,
// and the periodic job re-fetches the whole season every run, so one bad
// response corrupts the lot while the run still logs success. The same reasoning
// guards syncRankings against an empty poll.
var errNoRows = errors.New("no rows resolved from response")

// SyncTeamStats refreshes the pre-game team context the game detail page shows:
// SP+, FPI and CORE ratings, season win-loss records, against-the-spread
// records and season efficiency.
//
// Six requests, and nothing here is time-sensitive -- every source in it moves
// once a week, after Saturday. Each resource is attempted even when an earlier
// one failed, because they are independent and a page with four panels of six
// beats a page with none.
func (s *SyncService) SyncTeamStats(ctx context.Context, year int) error {
	s.logger.Info("syncing team stats", "season", year)

	var errs []error
	if err := s.syncSPRatings(ctx, year); err != nil {
		errs = append(errs, fmt.Errorf("sp+ ratings: %w", err))
	}
	if err := s.syncFPIRatings(ctx, year); err != nil {
		errs = append(errs, fmt.Errorf("fpi ratings: %w", err))
	}
	if err := s.syncCoreRatings(ctx, year); err != nil {
		errs = append(errs, fmt.Errorf("core ratings: %w", err))
	}
	if err := s.syncTeamRecords(ctx, year); err != nil {
		errs = append(errs, fmt.Errorf("records: %w", err))
	}
	if err := s.syncATSRecords(ctx, year); err != nil {
		errs = append(errs, fmt.Errorf("ats records: %w", err))
	}
	if err := s.syncAdvancedStats(ctx, year); err != nil {
		errs = append(errs, fmt.Errorf("advanced stats: %w", err))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	s.logger.Info("synced team stats", "season", year)
	return nil
}

func (s *SyncService) syncSPRatings(ctx context.Context, year int) error {
	payload, err := s.client.GetSPRatings(ctx, year)
	if err != nil {
		return err
	}

	resolver := s.newTeamResolver("sp+")
	return s.writeRatings(models.RatingSourceSP, spRatingsFrom(year, payload, resolver.byName), resolver)
}

func (s *SyncService) syncFPIRatings(ctx context.Context, year int) error {
	payload, err := s.client.GetFPIRatings(ctx, year)
	if err != nil {
		return err
	}

	resolver := s.newTeamResolver("fpi")
	return s.writeRatings(models.RatingSourceFPI, fpiRatingsFrom(year, payload, resolver.byName), resolver)
}

func (s *SyncService) syncCoreRatings(ctx context.Context, year int) error {
	payload, err := s.client.GetCoreRatings(ctx, year)
	if err != nil {
		return err
	}

	resolver := s.newTeamResolver("core")
	return s.writeRatings(models.RatingSourceCORE, coreRatingsFrom(year, payload, resolver.byName), resolver)
}

func (s *SyncService) syncTeamRecords(ctx context.Context, year int) error {
	payload, err := s.client.GetRecords(ctx, year)
	if err != nil {
		return err
	}

	resolver := s.newTeamResolver("records")
	rows := teamRecordsFrom(year, payload, resolver.byExternalID)
	resolver.report(s)

	// A team the database has never heard of is data; a database that cannot
	// answer is not, and mapping past it would write a partial season while
	// reporting success.
	if err := resolver.err; err != nil {
		return err
	}
	if len(rows) == 0 {
		return errNoRows
	}

	fetchedAt := s.clock.Now()
	written := 0
	for _, row := range rows {
		row.FetchedAt = fetchedAt
		if err := s.teamRecordRepo.Upsert(&row); err != nil {
			s.logger.Error("failed to upsert team record", "team", row.TeamID, "error", err)
			continue
		}
		written++
	}

	s.logger.Info("synced team records", "written", written)
	return nil
}

// writeRatings upserts one source's rows, refusing to write an empty set.
func (s *SyncService) writeRatings(source models.RatingSource, rows []models.TeamRating, resolver *teamResolver) error {
	resolver.report(s)

	if err := resolver.err; err != nil {
		return err
	}
	if len(rows) == 0 {
		return errNoRows
	}

	fetchedAt := s.clock.Now()
	written := 0
	for _, row := range rows {
		row.FetchedAt = fetchedAt
		if err := s.teamRatingRepo.Upsert(&row); err != nil {
			s.logger.Error("failed to upsert team rating", "source", source, "team", row.TeamID, "error", err)
			continue
		}
		written++
	}

	s.logger.Info("synced team ratings", "source", source, "written", written)
	return nil
}

// spRatingsFrom maps a /ratings/sp payload onto rows, dropping what resolve
// cannot place.
//
// The nested offense and defense objects carry a dozen more fields than these --
// success rate, explosiveness, havoc -- and every one of them is null on this
// tier, for a completed season as readily as a live one. Only what the endpoint
// populates is read.
func spRatingsFrom(year int, payload []APITeamSP, resolve func(string) (uuid.UUID, bool)) []models.TeamRating {
	rows := make([]models.TeamRating, 0, len(payload))
	for _, r := range payload {
		// The league-average row is not a team and never will be. Skipped
		// before resolve sees it, so it does not sit in the unmatched-names
		// warning on every single run -- which is how a log that exists to
		// catch a provider rename becomes a log nobody reads.
		if r.Team == APINationalAveragesTeam {
			continue
		}

		// Overall is NOT NULL, and a null rating decoded into a float64 is not
		// an absent number but a zero one -- which on this scale is a real
		// value, roughly the league average. SPProjection then differences that
		// fabrication against the other side and prints a margin that looks
		// sourced. FPI and CORE guard the same way.
		if r.Rating == nil {
			continue
		}

		teamID, ok := resolve(r.Team)
		if !ok {
			continue
		}

		rows = append(rows, models.TeamRating{
			TeamID:       teamID,
			Season:       year,
			Source:       models.RatingSourceSP,
			Overall:      decimal.NewFromFloat(*r.Rating),
			OverallRank:  r.Ranking,
			Offense:      decimalPtr(r.Offense.Rating),
			OffenseRank:  r.Offense.Ranking,
			Defense:      decimalPtr(r.Defense.Rating),
			DefenseRank:  r.Defense.Ranking,
			SpecialTeams: decimalPtr(r.SpecialTeams.Rating),
		})
	}
	return rows
}

// fpiRatingsFrom maps a /ratings/fpi payload onto rows.
func fpiRatingsFrom(year int, payload []APITeamFPI, resolve func(string) (uuid.UUID, bool)) []models.TeamRating {
	rows := make([]models.TeamRating, 0, len(payload))
	for _, r := range payload {
		// Overall is NOT NULL, so a team with no headline number has nothing to
		// store. The efficiencies are percentiles and cannot stand in for it.
		if r.FPI == nil {
			continue
		}

		teamID, ok := resolve(r.Team)
		if !ok {
			continue
		}

		rows = append(rows, models.TeamRating{
			TeamID:             teamID,
			Season:             year,
			Source:             models.RatingSourceFPI,
			Overall:            decimal.NewFromFloat(*r.FPI),
			OverallRank:        r.ResumeRanks.FPI,
			Offense:            decimalPtr(r.Efficiencies.Offense),
			Defense:            decimalPtr(r.Efficiencies.Defense),
			SpecialTeams:       decimalPtr(r.Efficiencies.SpecialTeams),
			StrengthOfSchedule: r.ResumeRanks.StrengthOfSchedule,
			StrengthOfRecord:   r.ResumeRanks.StrengthOfRecord,
		})
	}
	return rows
}

// coreRatingsFrom maps a /ratings/core payload onto rows.
//
// CORE is the one source that reports what it has seen, so ThroughWeek and
// ModelVersion are carried across. They stay nil for the others rather than
// being derived: a guessed week would be indistinguishable from a reported one.
func coreRatingsFrom(year int, payload []APITeamCore, resolve func(string) (uuid.UUID, bool)) []models.TeamRating {
	rows := make([]models.TeamRating, 0, len(payload))
	for _, r := range payload {
		if r.Overall == nil {
			continue
		}

		teamID, ok := resolve(r.Team)
		if !ok {
			continue
		}

		row := models.TeamRating{
			TeamID:      teamID,
			Season:      year,
			Source:      models.RatingSourceCORE,
			Overall:     decimal.NewFromFloat(*r.Overall),
			Offense:     decimalPtr(r.Offense),
			Defense:     decimalPtr(r.Defense),
			ThroughWeek: r.ThroughWeek,
		}
		if r.ModelVersion != "" {
			version := r.ModelVersion
			row.ModelVersion = &version
		}
		rows = append(rows, row)
	}
	return rows
}

// teamRecordsFrom maps a /records payload onto rows.
//
// Resolution is by the provider's team id, which this endpoint supplies and the
// rating endpoints do not, so a school rename cannot lose a record the way it
// could lose a rating.
func teamRecordsFrom(year int, payload []APITeamRecords, resolve func(int64) (uuid.UUID, bool)) []models.TeamRecord {
	rows := make([]models.TeamRecord, 0, len(payload))
	for _, r := range payload {
		teamID, ok := resolve(r.TeamID)
		if !ok {
			continue
		}

		rows = append(rows, models.TeamRecord{
			TeamID:       teamID,
			Season:       year,
			Games:        r.Total.Games,
			Wins:         r.Total.Wins,
			Losses:       r.Total.Losses,
			Ties:         r.Total.Ties,
			HomeGames:    r.HomeGames.Games,
			HomeWins:     r.HomeGames.Wins,
			HomeLosses:   r.HomeGames.Losses,
			HomeTies:     r.HomeGames.Ties,
			AwayGames:    r.AwayGames.Games,
			AwayWins:     r.AwayGames.Wins,
			AwayLosses:   r.AwayGames.Losses,
			AwayTies:     r.AwayGames.Ties,
			ExpectedWins: decimalPtr(r.ExpectedWins),
		})
	}
	return rows
}

// teamResolver maps the provider's teams onto our rows and keeps a tally of the
// ones it could not place.
//
// The rating endpoints key on a school name rather than an id, so a provider
// rename shows up as rows silently going missing. Counting the misses and naming
// them in one log line is what makes that visible; errNoRows is the hard stop
// for the case where it happens to every team at once.
type teamResolver struct {
	resource string
	repo     *repository.TeamRepository
	cache    map[string]uuid.UUID
	misses   []string

	// err holds the first failure that was the database rather than the data.
	// A team we have never heard of is skippable; a database that cannot answer
	// is not, and mapping past it would write a partial season and call it a
	// success.
	err error
}

func (s *SyncService) newTeamResolver(resource string) *teamResolver {
	return &teamResolver{
		resource: resource,
		repo:     s.teamRepo,
		cache:    make(map[string]uuid.UUID),
	}
}

// byName resolves a provider school name.
func (r *teamResolver) byName(name string) (uuid.UUID, bool) {
	if id, ok := r.cache[name]; ok {
		return id, true
	}

	team, err := r.repo.FindByNameAndSport(name, models.SportFootball)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) && r.err == nil {
			r.err = fmt.Errorf("looking up team %q: %w", name, err)
		}
		r.misses = append(r.misses, name)
		return uuid.Nil, false
	}

	r.cache[name] = team.ID
	return team.ID, true
}

// byExternalID resolves a provider team id.
func (r *teamResolver) byExternalID(externalID int64) (uuid.UUID, bool) {
	team, err := r.repo.FindByExternalID(externalID, models.SportFootball)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) && r.err == nil {
			r.err = fmt.Errorf("looking up team %d: %w", externalID, err)
		}
		r.misses = append(r.misses, fmt.Sprint(externalID))
		return uuid.Nil, false
	}
	return team.ID, true
}

// maxLoggedMisses bounds the names carried in the warning.
//
// /records covers every division and returns nearly 700 rows, so a bad season
// argument or an unseeded teams table misses on almost all of them. The count is
// what says how bad it is; a log line holding 700 school names is one nobody
// reads and some collectors truncate.
const maxLoggedMisses = 10

func (r *teamResolver) report(s *SyncService) {
	if len(r.misses) == 0 {
		return
	}

	sample := r.misses
	if len(sample) > maxLoggedMisses {
		sample = sample[:maxLoggedMisses]
	}

	s.logger.Warn("unmatched teams",
		"resource", r.resource,
		"count", len(r.misses),
		"teams", sample,
	)
}

func (s *SyncService) syncATSRecords(ctx context.Context, year int) error {
	payload, err := s.client.GetATSRecords(ctx, year)
	if err != nil {
		return err
	}

	resolver := s.newTeamResolver("ats")
	rows := atsRecordsFrom(year, payload, resolver.byExternalID)
	resolver.report(s)

	if err := resolver.err; err != nil {
		return err
	}
	if len(rows) == 0 {
		return errNoRows
	}

	fetchedAt := s.clock.Now()
	written := 0
	for _, row := range rows {
		row.FetchedAt = fetchedAt
		if err := s.teamATSRecordRepo.Upsert(&row); err != nil {
			s.logger.Error("failed to upsert ats record", "team", row.TeamID, "error", err)
			continue
		}
		written++
	}

	s.logger.Info("synced ats records", "written", written)
	return nil
}

func (s *SyncService) syncAdvancedStats(ctx context.Context, year int) error {
	payload, err := s.client.GetAdvancedSeasonStats(ctx, year)
	if err != nil {
		return err
	}

	resolver := s.newTeamResolver("advanced")
	rows := advancedStatsFrom(year, payload, resolver.byName)
	resolver.report(s)

	if err := resolver.err; err != nil {
		return err
	}
	if len(rows) == 0 {
		return errNoRows
	}

	fetchedAt := s.clock.Now()
	written := 0
	for _, row := range rows {
		row.FetchedAt = fetchedAt
		if err := s.teamAdvancedStatsRepo.Upsert(&row); err != nil {
			s.logger.Error("failed to upsert advanced stats", "team", row.TeamID, "error", err)
			continue
		}
		written++
	}

	s.logger.Info("synced advanced stats", "written", written)
	return nil
}

// atsRecordsFrom maps a /teams/ats payload onto rows.
//
// Resolution is by the provider's team id, which this endpoint supplies, so a
// school rename cannot lose a record.
func atsRecordsFrom(year int, payload []APITeamATS, resolve func(int64) (uuid.UUID, bool)) []models.TeamATSRecord {
	rows := make([]models.TeamATSRecord, 0, len(payload))
	for _, r := range payload {
		teamID, ok := resolve(r.TeamID)
		if !ok {
			continue
		}

		rows = append(rows, models.TeamATSRecord{
			TeamID:         teamID,
			Season:         year,
			Games:          r.Games,
			ATSWins:        r.ATSWins,
			ATSLosses:      r.ATSLosses,
			ATSPushes:      r.ATSPushes,
			AvgCoverMargin: decimalPtr(r.AvgCoverMargin),
		})
	}
	return rows
}

// advancedStatsFrom maps a /stats/season/advanced payload onto rows.
//
// The endpoint returns around forty numbers per side; these twelve are the ones
// the efficiency panel draws, and the rest are left undecoded rather than
// stored against a use nobody has.
func advancedStatsFrom(year int, payload []APITeamAdvancedStats, resolve func(string) (uuid.UUID, bool)) []models.TeamAdvancedStats {
	rows := make([]models.TeamAdvancedStats, 0, len(payload))
	for _, r := range payload {
		teamID, ok := resolve(r.Team)
		if !ok {
			continue
		}

		rows = append(rows, models.TeamAdvancedStats{
			TeamID: teamID,
			Season: year,

			OffensePlays:                r.Offense.Plays,
			OffenseDrives:               r.Offense.Drives,
			OffensePPA:                  decimalPtr(r.Offense.PPA),
			OffenseSuccessRate:          decimalPtr(r.Offense.SuccessRate),
			OffenseExplosiveness:        decimalPtr(r.Offense.Explosiveness),
			OffenseLineYards:            decimalPtr(r.Offense.LineYards),
			OffensePointsPerOpportunity: decimalPtr(r.Offense.PointsPerOpportunity),
			OffenseHavoc:                decimalPtr(r.Offense.Havoc.Total),

			DefensePlays:                r.Defense.Plays,
			DefenseDrives:               r.Defense.Drives,
			DefensePPA:                  decimalPtr(r.Defense.PPA),
			DefenseSuccessRate:          decimalPtr(r.Defense.SuccessRate),
			DefenseExplosiveness:        decimalPtr(r.Defense.Explosiveness),
			DefenseLineYards:            decimalPtr(r.Defense.LineYards),
			DefensePointsPerOpportunity: decimalPtr(r.Defense.PointsPerOpportunity),
			DefenseHavoc:                decimalPtr(r.Defense.Havoc.Total),
		})
	}
	return rows
}
