package admin

import (
	"fmt"
	"strconv"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/scheduler"
)

// SystemHealth is the state of the app that an operator checks first when
// something looks stale.
type SystemHealth struct {
	Counts repository.Stats

	// CFBConfigured and CBBConfigured say whether each upstream has a key. A
	// missing key disables that sync entirely at startup, which otherwise looks
	// identical to a sync that is simply never finding anything.
	CFBConfigured bool
	CBBConfigured bool

	TimeZone string
	Env      string

	// Season, Week and SeasonType are what the app currently believes "now" is.
	// A wrong answer here is the failure that hides best: the syncs keep
	// reporting success while fetching a season that contains none of the games
	// anyone is watching, so scores never arrive and bets never settle.
	Season     int
	Week       int
	SeasonType models.SeasonType
	WeekFound  bool
}

// JobStatuses returns every registered job, including the internal ones that
// carry no Label and so stay out of the site footer.
func (s *Service) JobStatuses() []scheduler.Status {
	if s.sched == nil {
		return nil
	}
	return s.sched.Status()
}

// minSeedSeason is the earliest season worth asking either provider for. The
// football calendar endpoint returns nothing before 2002, so an earlier year is
// a typo rather than a backfill.
const minSeedSeason = 2002

// TriggerSync asks a job to run now.
//
// season is optional and applies to the seed jobs: empty means the season the
// app currently believes it is in, which is what a routine seed wants. Anything
// else is validated here rather than in the job, because a job runs on a
// background goroutine where a bad value can only be logged -- while here it can
// be shown to the person who typed it.
//
// It returns scheduler.ErrUnknownJob for a name that is not registered,
// scheduler.ErrRunPending when a run has already been requested and not yet
// started, and ErrInvalidSeason for a season outside the plausible range.
func (s *Service) TriggerSync(actor *models.User, job, season string) error {
	if s.sched == nil {
		return scheduler.ErrUnknownJob
	}

	var args []string
	detail := job

	if season != "" {
		year, err := strconv.Atoi(season)
		if err != nil || year < minSeedSeason || year > time.Now().Year()+1 {
			return ErrInvalidSeason
		}
		args = []string{season}
		detail = fmt.Sprintf("%s (season %s)", job, season)
	}

	if err := s.sched.Trigger(job, args...); err != nil {
		return err
	}

	s.audit(actor, models.AuditActionSyncTriggered, models.AuditTargetSync, nil, detail)
	return nil
}

// Health gathers the row counts and configuration shown on the sync page.
func (s *Service) Health() (SystemHealth, error) {
	counts, err := s.statsRepo.Counts()
	if err != nil {
		return SystemHealth{}, err
	}

	health := SystemHealth{
		Counts:        counts,
		CFBConfigured: s.cfg.CFBDataAPIKey != "",
		CBBConfigured: s.cfg.CBBDataAPIKey != "",
		TimeZone:      s.cfg.TimeZone,
		Env:           s.cfg.Env,
	}

	// GetCurrentWeek already drops weeks whose span cannot be real, which is the
	// rule every "what season is it" path has to apply.
	health.Season, health.Week, health.SeasonType, health.WeekFound = s.games.GetCurrentWeek()

	return health, nil
}
