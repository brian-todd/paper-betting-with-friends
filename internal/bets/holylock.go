package bets

// Holy Lock is a per-user, per-league, per-week designation of the one bet its
// owner is most confident in. It is display only: nothing in this file reads or
// writes a purse, a stake, an odds snapshot or a bet status, and settlement is
// unaware the flag exists.

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	// ErrBetNotFootballWeek is returned for a bet whose game has no week row.
	// Basketball games carry a season but no week, so their bets have no
	// per-week slot to occupy.
	ErrBetNotFootballWeek = errors.New("bet is not on a game with a football week")
	// ErrHolyLockSettled is returned when the week's designation can no longer
	// move because the bet holding it has already kicked off.
	ErrHolyLockSettled = errors.New("this week's holy lock is already locked in")
	// ErrHolyLockExists is returned when placing a bet as a Holy Lock would take
	// a week's designation another bet already holds.
	//
	// Placement refuses rather than reassigning, which is what moving the lock
	// from the bets page does. Nominating at placement is a decision about the
	// bet being written; silently unmarking a different one as a side effect of
	// it is not something the reader asked for.
	ErrHolyLockExists = errors.New("a holy lock is already set for this week")
)

// holyLockTarget is the slice of a bet the Holy Lock rules read, resolved from
// whichever of the three tables holds it.
type holyLockTarget struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	LeagueID uuid.UUID
	Status   models.BetStatus
	WeekID   *uuid.UUID // games.week_id; nil for basketball
	Kickoff  time.Time
}

// findHolyLockTarget loads a bet by type and id. Each repository's FindByID
// already preloads Game, and WeekID and ScheduledAt are plain columns on it, so
// this costs one query rather than two.
func (s *Service) findHolyLockTarget(betType string, betID uuid.UUID) (*holyLockTarget, error) {
	switch betType {
	case BetTypeSpread:
		bet, err := s.spreadBetRepo.FindByID(betID)
		if err != nil {
			return nil, wrapBetLookup(err)
		}
		return &holyLockTarget{bet.ID, bet.UserID, bet.LeagueID, bet.Status, bet.Game.WeekID, bet.Game.ScheduledAt}, nil
	case BetTypeMoneyLine:
		bet, err := s.moneyLineBetRepo.FindByID(betID)
		if err != nil {
			return nil, wrapBetLookup(err)
		}
		return &holyLockTarget{bet.ID, bet.UserID, bet.LeagueID, bet.Status, bet.Game.WeekID, bet.Game.ScheduledAt}, nil
	case BetTypeOverUnder:
		bet, err := s.overUnderBetRepo.FindByID(betID)
		if err != nil {
			return nil, wrapBetLookup(err)
		}
		return &holyLockTarget{bet.ID, bet.UserID, bet.LeagueID, bet.Status, bet.Game.WeekID, bet.Game.ScheduledAt}, nil
	default:
		return nil, ErrInvalidBetType
	}
}

// authorizeHolyLock is the gate both setting and clearing pass through.
//
// It mirrors authorizeEdit -- ownership, still pending, and the game not yet
// kicked off, read from ScheduledAt rather than Game.Status because status only
// advances when the sync runs. Two gates are its own: the game must belong to a
// football week, and the caller must still be a member of the league, which
// they may have left since placing the bet.
func (s *Service) authorizeHolyLock(betType string, betID, userID uuid.UUID) (*holyLockTarget, error) {
	target, err := s.findHolyLockTarget(betType, betID)
	if err != nil {
		return nil, err
	}
	if target.UserID != userID {
		return nil, ErrNotBetOwner
	}
	if target.Status != models.BetStatusPending {
		return nil, ErrBetNotPending
	}

	isMember, err := s.leagueRepo.IsMember(target.LeagueID, userID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrNotLeagueMember
	}

	if target.WeekID == nil {
		return nil, ErrBetNotFootballWeek
	}
	if !target.Kickoff.After(time.Now()) {
		return nil, ErrGameStarted
	}
	return target, nil
}

// SetHolyLock designates a pending bet as the caller's Holy Lock for its league
// and week, releasing whatever bet held that slot.
func (s *Service) SetHolyLock(betType string, betID, userID uuid.UUID) error {
	target, err := s.authorizeHolyLock(betType, betID, userID)
	if err != nil {
		return err
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		// One lock per week is not a constraint the database can hold -- a
		// bet's week lives two joins away, on its game -- so serialize the
		// designations for this user and league instead. Without it a double
		// submit can clear and set concurrently and leave two.
		key := userID.String() + ":" + target.LeagueID.String()
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
			return err
		}

		// Repositories are built from a *gorm.DB, so the transaction is threaded
		// by constructing against tx rather than by passing it down every call.
		repo := repository.NewHolyLockRepository(tx)

		slot, err := repo.FindSlot(userID, target.LeagueID, *target.WeekID)
		if err != nil {
			return err
		}
		if slot != nil && slot.BetID != betID && !slot.ScheduledAt.After(time.Now()) {
			return ErrHolyLockSettled
		}

		if err := repo.ClearWeek(userID, target.LeagueID, *target.WeekID); err != nil {
			return err
		}
		return repo.Set(betType, betID)
	})
}

// ClearHolyLock drops the designation, on the same gates as setting it.
func (s *Service) ClearHolyLock(betType string, betID, userID uuid.UUID) error {
	target, err := s.authorizeHolyLock(betType, betID, userID)
	if err != nil {
		return err
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		key := userID.String() + ":" + target.LeagueID.String()
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
			return err
		}
		return repository.NewHolyLockRepository(tx).ClearWeek(userID, target.LeagueID, *target.WeekID)
	})
}

// holyLockWeek identifies one league's week. The league is part of the key
// because the designation is per league: the same week is a separate slot in
// each one the user plays in.
type holyLockWeek struct {
	league uuid.UUID
	week   uuid.UUID
}

// frozenHolyLockWeeks reports the league weeks whose Holy Lock can no longer
// move, because the bet holding it has already kicked off.
func frozenHolyLockWeeks(slots []repository.HolyLockSlot, now time.Time) map[holyLockWeek]bool {
	frozen := make(map[holyLockWeek]bool, len(slots))
	for _, slot := range slots {
		if !slot.ScheduledAt.After(now) {
			frozen[holyLockWeek{league: slot.LeagueID, week: slot.WeekID}] = true
		}
	}
	return frozen
}

// holyLockEligible mirrors the gates authorizeHolyLock enforces. The two must
// agree, or the page offers a designation the service then refuses -- the same
// contract editable() has with authorizeEdit.
//
// A bet whose own game has started fails editable() already, so frozen only
// ever suppresses the *other* bets in a week whose lock has kicked off.
func holyLockEligible(status models.BetStatus, game models.Game, frozen bool, now time.Time) bool {
	return editable(status, game, now) && game.WeekID != nil && !frozen
}

// markHolyLockEligibility fills in HolyLockEligible across a user's own bet
// list.
//
// It costs one query for every lock the user holds rather than one per bet: a
// week whose marked game has kicked off is closed to the rest of that week's
// bets, and that is not knowable from a bet row on its own.
func (s *Service) markHolyLockEligibility(userID uuid.UUID, bets []BetView) {
	slots, err := s.holyLockRepo.FindSlotsByUser(userID)
	if err != nil {
		// Losing this costs the reader the Holy Lock buttons, not the page.
		slog.Error("failed to load holy lock slots", "user", userID, "error", err)
		return
	}

	frozen := frozenHolyLockWeeks(slots, time.Now())
	now := time.Now()
	for i := range bets {
		if bets[i].Game.WeekID == nil {
			continue
		}
		// League.ID rather than a bare LeagueID: BetView carries the league only
		// as the preloaded association, which FindFiltered populates.
		key := holyLockWeek{league: bets[i].League.ID, week: *bets[i].Game.WeekID}
		bets[i].HolyLockEligible = holyLockEligible(bets[i].Status, bets[i].Game, frozen[key], now)
	}
}

// ensureHolyLockAvailable reports whether a bet about to be placed may claim its
// week's designation.
//
// Callers run this before deducting the stake, so a refusal moves no money and
// needs no compensating refund.
func (s *Service) ensureHolyLockAvailable(userID, leagueID uuid.UUID, game models.Game) error {
	if game.WeekID == nil {
		return ErrBetNotFootballWeek
	}

	slot, err := s.holyLockRepo.FindSlot(userID, leagueID, *game.WeekID)
	if err != nil {
		return err
	}
	if slot != nil {
		return ErrHolyLockExists
	}
	return nil
}

// DescribeHolyLock names the Holy Lock a user already holds in the league and
// week the given game belongs to, or "" when there is none.
//
// It exists for the message shown when placement is refused: the sentinel error
// cannot carry the detail, and looking it up on the error path costs a query
// only when the placement was going to fail anyway.
func (s *Service) DescribeHolyLock(userID, leagueID, gameID uuid.UUID) string {
	game, err := s.gameRepo.FindByID(gameID)
	if err != nil || game.WeekID == nil {
		return ""
	}

	row, err := s.holyLockRepo.FindLockInWeek(userID, leagueID, *game.WeekID)
	if err != nil || row == nil {
		return ""
	}
	return describeHolyLock(*row)
}

// HolyLockConflicts maps a league ID to the Holy Lock the user already holds in
// the week of the given game, for the leagues where that slot is taken. A league
// missing from the map still has its designation free.
//
// The bet slip uses this to say so before the reader fills the form in, rather
// than only refusing on submit. It costs one query for the user's locks plus one
// per league that actually has a conflict, which is nearly always none or one.
func (s *Service) HolyLockConflicts(userID uuid.UUID, game models.Game) (map[string]string, error) {
	if game.WeekID == nil {
		return nil, nil
	}

	slots, err := s.holyLockRepo.FindSlotsByUser(userID)
	if err != nil {
		return nil, err
	}

	conflicts := make(map[string]string)
	for _, slot := range slots {
		if slot.WeekID != *game.WeekID {
			continue
		}
		row, err := s.holyLockRepo.FindLockInWeek(userID, slot.LeagueID, slot.WeekID)
		if err != nil {
			return nil, err
		}
		if row != nil {
			conflicts[slot.LeagueID.String()] = describeHolyLock(*row)
		}
	}
	return conflicts, nil
}

// describeHolyLock renders a lock as one line naming the pick and the matchup,
// e.g. "GT -7 (CLEM @ GT)".
func describeHolyLock(row repository.LeagueHolyLockRow) string {
	return HolyLockPick(row) + " (" + row.AwayAbbr + " @ " + row.HomeAbbr + ")"
}

// HolyLockPick renders the pick on a Holy Lock the way the bets table does: the
// team plus the line for a spread, the team plus the odds for a money line, and
// the side plus the total for an over/under.
//
// It is exported because the league page renders the same rows, and two copies
// of this would be free to disagree about what a bet says.
func HolyLockPick(row repository.LeagueHolyLockRow) string {
	side := row.HomeAbbr
	if row.Pick == string(models.SpreadPickAway) {
		side = row.AwayAbbr
	}

	switch row.BetType {
	case BetTypeSpread:
		spread, text := holyLockLine(row.LineValue)
		if spread.IsPositive() {
			text = "+" + text
		}
		return strings.TrimSpace(side + " " + text)
	case BetTypeMoneyLine:
		return side + " " + formatOdds(row.OddsSnapshot)
	case BetTypeOverUnder:
		_, total := holyLockLine(row.LineValue)
		pick := "Over"
		if row.Pick == string(models.OverUnderPickUnder) {
			pick = "Under"
		}
		return strings.TrimSpace(pick + " " + total)
	default:
		return side
	}
}

// holyLockLine parses the line the union query returns as text. decimal.String
// drops the trailing zero the numeric column carries, so a whole-number line
// reads "-7" rather than "-7.0" while a half-point line keeps its ".5".
func holyLockLine(value *string) (decimal.Decimal, string) {
	if value == nil {
		return decimal.Zero, ""
	}
	d, err := decimal.NewFromString(*value)
	if err != nil {
		return decimal.Zero, *value
	}
	return d, d.String()
}
