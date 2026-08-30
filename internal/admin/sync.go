package admin

import (
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

// TriggerSync asks a job to run now. It returns scheduler.ErrUnknownJob for a
// name that is not registered and scheduler.ErrRunPending when a run has
// already been requested and not yet started.
func (s *Service) TriggerSync(actor *models.User, job string) error {
	if s.sched == nil {
		return scheduler.ErrUnknownJob
	}
	if err := s.sched.Trigger(job); err != nil {
		return err
	}

	s.audit(actor, models.AuditActionSyncTriggered, models.AuditTargetSync, nil, job)
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
