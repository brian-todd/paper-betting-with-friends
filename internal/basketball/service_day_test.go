package basketball

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/brian/paper-betting-with-friends/internal/timeutil"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// dayRig is a basketball table a test fills by hand. Each case is about how the
// page reads a day, so the inputs need to be visible rather than captured.
type dayRig struct {
	t   *testing.T
	db  *gorm.DB
	svc *Service
	loc *time.Location
}

func newDayRig(t *testing.T, now time.Time) *dayRig {
	t.Helper()
	loc := mustLoad(t, "America/New_York")
	db := testdb.Open(t)
	svc := NewService(db, loc)
	svc.SetClock(timeutil.Fixed(now))
	return &dayRig{t: t, db: db, svc: svc, loc: loc}
}

func (r *dayRig) team(conference string) *models.Team {
	r.t.Helper()
	team := testdb.InsertTeam(r.t, r.db, "")
	if err := r.db.Model(team).Updates(map[string]any{"conference": conference, "sport": models.SportBasketball}).Error; err != nil {
		r.t.Fatalf("updating team: %v", err)
	}
	return team
}

func (r *dayRig) game(at time.Time, season int, home, away *models.Team, withLine bool) *models.Game {
	r.t.Helper()
	game := testdb.InsertGame(r.t, r.db, models.Game{
		Sport: models.SportBasketball, Season: season, ScheduledAt: at,
		HomeTeamID: home.ID, AwayTeamID: away.ID,
	})
	if withLine {
		line := &models.SpreadOdds{
			GameID: game.ID, Source: models.OddsSourceDraftKings,
			HomeSpread: decimal.NewFromInt(-3), AwaySpread: decimal.NewFromInt(3),
			HomeOdds: decimal.NewFromInt(-110), AwayOdds: decimal.NewFromInt(-110),
		}
		if err := r.db.Create(line).Error; err != nil {
			r.t.Fatalf("creating line: %v", err)
		}
	}
	return game
}

func (r *dayRig) on(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 19, 0, 0, 0, r.loc)
}

func TestGetDayShowsGamesWithLinesWhenTheDayHasThem(t *testing.T) {
	r := newDayRig(t, time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC))
	acc, sec, d3 := r.team("ACC"), r.team("SEC"), r.team(noConference)

	r.game(r.on(time.November, 5), 2027, acc, sec, true)
	r.game(r.on(time.November, 5), 2027, sec, d3, false)
	r.game(r.on(time.November, 6), 2027, acc, sec, false)

	t.Run("a day with lines hides the rest and says how many", func(t *testing.T) {
		day, err := r.svc.GetDay(r.on(time.November, 5), DayQuery{})
		if err != nil {
			t.Fatalf("GetDay: %v", err)
		}
		if len(day.Games) != 1 || !day.LinesOnly || day.HiddenGames != 1 || day.Total != 2 {
			t.Errorf("games = %d, linesOnly = %v, hidden = %d, total = %d", len(day.Games), day.LinesOnly, day.HiddenGames, day.Total)
		}
		if day.Games[0].Spread == nil {
			t.Error("the game's line was not attached")
		}
	})

	t.Run("asking for all of them shows all of them", func(t *testing.T) {
		day, err := r.svc.GetDay(r.on(time.November, 5), DayQuery{AllGames: true})
		if err != nil {
			t.Fatalf("GetDay: %v", err)
		}
		if len(day.Games) != 2 || day.LinesOnly {
			t.Errorf("games = %d, linesOnly = %v", len(day.Games), day.LinesOnly)
		}
	})

	t.Run("a day with no lines yet still shows its games", func(t *testing.T) {
		day, err := r.svc.GetDay(r.on(time.November, 6), DayQuery{})
		if err != nil {
			t.Fatalf("GetDay: %v", err)
		}
		if len(day.Games) != 1 || day.LinesOnly {
			t.Errorf("games = %d, linesOnly = %v; books post late, so this must not read as empty", len(day.Games), day.LinesOnly)
		}
	})

	t.Run("a filter with no lined games still finds its match", func(t *testing.T) {
		day, err := r.svc.GetDay(r.on(time.November, 5), DayQuery{Search: d3.Name})
		if err != nil {
			t.Fatalf("GetDay: %v", err)
		}
		if len(day.Games) != 1 {
			t.Errorf("games = %d, want the unlined game the search asked for", len(day.Games))
		}
	})

	t.Run("the conference filter matches either side and lists real conferences", func(t *testing.T) {
		day, err := r.svc.GetDay(r.on(time.November, 5), DayQuery{Conference: "SEC", AllGames: true})
		if err != nil {
			t.Fatalf("GetDay: %v", err)
		}
		if len(day.Games) != 2 {
			t.Errorf("games = %d, want both games with an SEC side", len(day.Games))
		}
		if len(day.Conferences) != 2 || day.Conferences[0] != "ACC" || day.Conferences[1] != "SEC" {
			t.Errorf("conferences = %v, want ACC and SEC without %q", day.Conferences, noConference)
		}
	})
}

func TestGetDayPagerSkipsDaysWithoutGames(t *testing.T) {
	r := newDayRig(t, time.Date(2026, 12, 25, 12, 0, 0, 0, time.UTC))
	home, away := r.team("ACC"), r.team("SEC")

	// The holiday break.
	r.game(r.on(time.December, 23), 2027, home, away, false)
	r.game(r.on(time.December, 27), 2027, home, away, false)
	// A late tip is still the 27th in Eastern, though it is the 28th in UTC.
	r.game(time.Date(2026, 12, 27, 23, 30, 0, 0, r.loc), 2027, home, away, false)

	day, err := r.svc.GetDay(r.on(time.December, 25), DayQuery{})
	if err != nil {
		t.Fatalf("GetDay: %v", err)
	}
	if day.PrevDay == nil || day.PrevDay.Format(time.DateOnly) != "2026-12-23" {
		t.Errorf("PrevDay = %v, want the 23rd", day.PrevDay)
	}
	if day.NextDay == nil || day.NextDay.Format(time.DateOnly) != "2026-12-27" {
		t.Errorf("NextDay = %v, want the 27th", day.NextDay)
	}

	last, err := r.svc.GetDay(r.on(time.December, 27), DayQuery{})
	if err != nil {
		t.Fatalf("GetDay: %v", err)
	}
	if last.NextDay != nil {
		t.Errorf("NextDay = %v, want none past the last game", last.NextDay)
	}
	if len(last.Games) != 2 {
		t.Errorf("games on the 27th = %d, want 2", len(last.Games))
	}
}

func TestDefaultDayAndDayInSeason(t *testing.T) {
	seed := func(r *dayRig) {
		home, away := r.team("ACC"), r.team("SEC")
		r.game(time.Date(2025, 11, 3, 19, 0, 0, 0, r.loc), 2026, home, away, false)
		r.game(time.Date(2026, 4, 6, 21, 0, 0, 0, r.loc), 2026, home, away, false)
		r.game(time.Date(2026, 11, 3, 19, 0, 0, 0, r.loc), 2027, home, away, false)
		r.game(time.Date(2026, 11, 6, 19, 0, 0, 0, r.loc), 2027, home, away, false)
	}

	tests := []struct {
		name   string
		now    time.Time
		season int
		want   string
	}{
		{"a finished season opens on its last day", time.Date(2026, 11, 4, 17, 0, 0, 0, time.UTC), 2026, "2026-04-06"},
		{"the current season opens on the next game day", time.Date(2026, 11, 4, 17, 0, 0, 0, time.UTC), 2027, "2026-11-06"},
		{"a season yet to start opens on its first day", time.Date(2026, 9, 1, 17, 0, 0, 0, time.UTC), 2027, "2026-11-03"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newDayRig(t, tt.now)
			seed(r)
			day, ok, err := r.svc.DayInSeason(tt.season)
			if err != nil || !ok {
				t.Fatalf("DayInSeason = %v, %v", ok, err)
			}
			if got := day.Format(time.DateOnly); got != tt.want {
				t.Errorf("day = %s, want %s", got, tt.want)
			}
		})
	}

	t.Run("a season with no games is not a day", func(t *testing.T) {
		r := newDayRig(t, time.Date(2026, 11, 4, 17, 0, 0, 0, time.UTC))
		if _, ok, err := r.svc.DayInSeason(2040); err != nil || ok {
			t.Errorf("DayInSeason = %v, %v, want not found", ok, err)
		}
	})

	t.Run("the default day lands on games between seasons", func(t *testing.T) {
		r := newDayRig(t, time.Date(2026, 8, 1, 17, 0, 0, 0, time.UTC))
		seed(r)
		day, err := r.svc.DefaultDay()
		if err != nil {
			t.Fatalf("DefaultDay: %v", err)
		}
		if got := day.Format(time.DateOnly); got != "2026-11-03" {
			t.Errorf("day = %s, want the next season's first", got)
		}
	})
}

func TestGetDayAttachesThePollAsOfThatDay(t *testing.T) {
	r := newDayRig(t, time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
	home, away := r.team("ACC"), r.team("SEC")

	poll := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	if err := r.db.Create(&models.PollRank{TeamID: home.ID, Season: 2027, Poll: models.PollAP, PollDate: poll, Rank: 4}).Error; err != nil {
		t.Fatalf("creating rank: %v", err)
	}

	r.game(r.on(time.November, 5), 2027, home, away, false)
	r.game(r.on(time.November, 10), 2027, home, away, false)
	// Sunday, the night before the poll's date. The poll arrives as Monday 00:00
	// UTC, which is 7pm this Sunday in Eastern, but it had not been published.
	r.game(time.Date(2026, 11, 8, 21, 0, 0, 0, r.loc), 2027, home, away, false)

	sunday, err := r.svc.GetDay(r.on(time.November, 8), DayQuery{})
	if err != nil {
		t.Fatalf("GetDay: %v", err)
	}
	if sunday.Games[0].HomeRank != nil {
		t.Errorf("a Sunday game reads Monday's poll, rank %d", *sunday.Games[0].HomeRank)
	}

	before, err := r.svc.GetDay(r.on(time.November, 5), DayQuery{})
	if err != nil {
		t.Fatalf("GetDay: %v", err)
	}
	if before.Games[0].HomeRank != nil {
		t.Errorf("a game before the poll reads rank %d", *before.Games[0].HomeRank)
	}

	after, err := r.svc.GetDay(r.on(time.November, 10), DayQuery{})
	if err != nil {
		t.Fatalf("GetDay: %v", err)
	}
	if after.Games[0].HomeRank == nil || *after.Games[0].HomeRank != 4 {
		t.Errorf("HomeRank = %v, want 4", after.Games[0].HomeRank)
	}
	if after.Games[0].AwayRank != nil {
		t.Errorf("AwayRank = %d, want unranked", *after.Games[0].AwayRank)
	}
}

// One bettor's custom line is not a line the day offers, so it must not hide the
// slate behind one game showing no odds.
func TestGetDayIgnoresCustomLinesWhenDecidingWhatToShow(t *testing.T) {
	line := decimal.NewFromInt(-110)
	tests := []struct {
		name   string
		custom func(gameID uuid.UUID) any
	}{
		{"spread", func(id uuid.UUID) any {
			return &models.SpreadOdds{GameID: id, Source: models.OddsSourceCustom, HomeSpread: decimal.NewFromInt(-3), AwaySpread: decimal.NewFromInt(3), HomeOdds: line, AwayOdds: line}
		}},
		{"money line", func(id uuid.UUID) any {
			return &models.MoneyLineOdds{GameID: id, Source: models.OddsSourceCustom, HomeOdds: line, AwayOdds: line}
		}},
		{"total", func(id uuid.UUID) any {
			return &models.OverUnderOdds{GameID: id, Source: models.OddsSourceCustom, Total: decimal.NewFromInt(140), OverOdds: line, UnderOdds: line}
		}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newDayRig(t, time.Date(2026, 12, 5, 12, 0, 0, 0, time.UTC))
			date := r.on(time.December, 6+i)
			a, b := r.team("ACC"), r.team("SEC")
			withCustom := r.game(date, 2027, a, b, false)
			r.game(date, 2027, b, a, false)

			if err := r.db.Create(tt.custom(withCustom.ID)).Error; err != nil {
				t.Fatalf("creating custom line: %v", err)
			}

			day, err := r.svc.GetDay(date, DayQuery{})
			if err != nil {
				t.Fatalf("GetDay: %v", err)
			}
			if day.LinesOnly || len(day.Games) != 2 {
				t.Errorf("linesOnly = %v with %d games, want the whole slate", day.LinesOnly, len(day.Games))
			}
		})
	}
}

// The pager carries a conference from day to day, onto days without it.
func TestGetDayKeepsTheSelectedConferenceInTheDropdown(t *testing.T) {
	r := newDayRig(t, time.Date(2026, 12, 5, 12, 0, 0, 0, time.UTC))
	r.game(r.on(time.December, 6), 2027, r.team("SEC"), r.team("Big Ten"), false)

	tests := []struct {
		selected string
		want     []string
	}{
		{"", []string{"Big Ten", "SEC"}},
		{"SEC", []string{"Big Ten", "SEC"}},
		{"ACC", []string{"ACC", "Big Ten", "SEC"}},
	}
	for _, tt := range tests {
		day, err := r.svc.GetDay(r.on(time.December, 6), DayQuery{Conference: tt.selected})
		if err != nil {
			t.Fatalf("GetDay: %v", err)
		}
		if !slices.Equal(day.Conferences, tt.want) {
			t.Errorf("selected %q: conferences = %v, want %v", tt.selected, day.Conferences, tt.want)
		}
	}
}

// Ranks decorate a page whose games have loaded. Refused by a cancelled context so
// the test's transaction survives to read the rest of the page.
func TestGetDaySurvivesFailingToReadRanks(t *testing.T) {
	r := newDayRig(t, time.Date(2026, 12, 5, 12, 0, 0, 0, time.UTC))
	home := r.team("ACC")
	r.game(r.on(time.December, 6), 2027, home, r.team("SEC"), false)
	if err := r.db.Create(&models.PollRank{
		TeamID: home.ID, Season: 2027, Poll: models.PollAP,
		PollDate: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), Rank: 7,
	}).Error; err != nil {
		t.Fatalf("creating rank: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	r.svc.pollRankRepo = repository.NewPollRankRepository(r.db.WithContext(cancelled))

	day, err := r.svc.GetDay(r.on(time.December, 6), DayQuery{})
	if err != nil {
		t.Fatalf("GetDay: %v", err)
	}
	if len(day.Games) != 1 || day.Games[0].HomeRank != nil {
		t.Errorf("games = %+v, want the game without its rank", day.Games)
	}
	if len(day.Conferences) != 2 {
		t.Errorf("conferences = %v, want the rest of the page intact", day.Conferences)
	}
}
