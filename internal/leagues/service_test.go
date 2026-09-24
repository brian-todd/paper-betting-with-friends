package leagues

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/brian/paper-betting-with-friends/internal/testdb"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

//go:fix inline
func intPtr(v int) *int { return new(v) }

func TestBuildWeeklyStats(t *testing.T) {
	me := uuid.New()
	alice := uuid.New()
	zed := uuid.New()

	stake := decimal.RequireFromString("1000")

	row := func(user uuid.UUID, name string, season, week *int, status, odds string) repository.LeagueBetRow {
		return repository.LeagueBetRow{
			UserID:       user,
			Username:     name,
			Season:       season,
			Week:         week,
			Status:       status,
			Stake:        stake,
			OddsSnapshot: decimal.RequireFromString(odds),
		}
	}

	rows := []repository.LeagueBetRow{
		// Week 1: my won/lost/push/pending mix, plus a void that must vanish.
		row(me, "Me", new(2026), new(1), "won", "129"),
		row(me, "Me", new(2026), new(1), "lost", "-110"),
		row(me, "Me", new(2026), new(1), "push", "-110"),
		row(me, "Me", new(2026), new(1), "pending", "-110"),
		row(me, "Me", new(2026), new(1), "void", "-110"),
		// Week 1: other members, out of alphabetical order.
		row(zed, "Zed", new(2026), new(1), "won", "-200"),
		row(alice, "alice", new(2026), new(1), "lost", "100"),
		// Week 2, newer, should sort first.
		row(alice, "alice", new(2026), new(2), "won", "150"),
		// A bet whose game has no calendar week, should sort last.
		row(me, "Me", nil, nil, "pending", "-110"),
	}

	weeks := buildWeeklyStats(rows, me)

	if len(weeks) != 3 {
		t.Fatalf("got %d week groups, want 3", len(weeks))
	}

	t.Run("week ordering newest first, unscheduled last", func(t *testing.T) {
		wantLabels := []string{"2026 · Week 2", "2026 · Week 1", "Unscheduled"}
		for i, want := range wantLabels {
			if weeks[i].Label != want {
				t.Errorf("weeks[%d].Label = %q, want %q", i, weeks[i].Label, want)
			}
		}
	})

	t.Run("current user first then case-insensitive alphabetical", func(t *testing.T) {
		week1 := weeks[1]
		if len(week1.Rows) != 3 {
			t.Fatalf("week 1 has %d rows, want 3", len(week1.Rows))
		}
		gotOrder := []string{week1.Rows[0].Username, week1.Rows[1].Username, week1.Rows[2].Username}
		wantOrder := []string{"Me", "alice", "Zed"}
		for i := range wantOrder {
			if gotOrder[i] != wantOrder[i] {
				t.Fatalf("week 1 row order = %v, want %v", gotOrder, wantOrder)
			}
		}
		if !week1.Rows[0].IsCurrentUser {
			t.Error("first row should be the current user")
		}
	})

	t.Run("aggregates counts and money", func(t *testing.T) {
		mine := weeks[1].Rows[0]
		if mine.Wins != 1 || mine.Losses != 1 || mine.Pushes != 1 || mine.Pending != 1 {
			t.Errorf("record = %dW-%dL-%dP, %d pending; want 1W-1L-1P, 1 pending",
				mine.Wins, mine.Losses, mine.Pushes, mine.Pending)
		}
		// Void bet excluded: 4 x 1000 staked, not 5.
		if want := decimal.RequireFromString("4000"); !mine.Staked.Equal(want) {
			t.Errorf("Staked = %s, want %s", mine.Staked, want)
		}
		// Won at +129 pays 2290.00; push refunds the 1000 stake.
		if want := decimal.RequireFromString("3290.00"); !mine.Winnings.Equal(want) {
			t.Errorf("Winnings = %s, want %s", mine.Winnings, want)
		}
		// Net: +1290 profit, -1000 lost, push and pending contribute nothing.
		if want := decimal.RequireFromString("290.00"); !mine.Net.Equal(want) {
			t.Errorf("Net = %s, want %s", mine.Net, want)
		}
	})

	t.Run("negative odds payout", func(t *testing.T) {
		zedRow := weeks[1].Rows[2]
		// Won at -200: 1000 stake returns 1500.
		if want := decimal.RequireFromString("1500.00"); !zedRow.Winnings.Equal(want) {
			t.Errorf("Winnings = %s, want %s", zedRow.Winnings, want)
		}
		if want := decimal.RequireFromString("500.00"); !zedRow.Net.Equal(want) {
			t.Errorf("Net = %s, want %s", zedRow.Net, want)
		}
	})
}

func TestBuildHolyLockWeeks(t *testing.T) {
	me := uuid.New()
	alice := uuid.New()
	zed := uuid.New()

	line := func(v string) *string { return &v }
	row := func(user uuid.UUID, name string, week int, seasonType, betType, pick string, lineValue *string, odds string) repository.LeagueHolyLockRow {
		return repository.LeagueHolyLockRow{
			UserID:       user,
			Username:     name,
			Season:       2026,
			Week:         week,
			SeasonType:   seasonType,
			BetType:      betType,
			Status:       "pending",
			Pick:         pick,
			LineValue:    lineValue,
			OddsSnapshot: decimal.RequireFromString(odds),
			Stake:        decimal.RequireFromString("50"),
			HomeAbbr:     "GT",
			AwayAbbr:     "CLEM",
		}
	}

	rows := []repository.LeagueHolyLockRow{
		// Week 1, deliberately out of alphabetical order.
		row(zed, "zed", 1, "regular", "overunder", "over", line("54.5"), "-110"),
		row(alice, "Alice", 1, "regular", "moneyline", "away", nil, "150"),
		row(me, "Me", 1, "regular", "spread", "home", line("-7.0"), "-110"),
		// Week 2.
		row(me, "Me", 2, "regular", "spread", "away", line("3.5"), "-110"),
		// Postseason week 1 must not merge with regular week 1.
		row(me, "Me", 1, "postseason", "overunder", "under", line("48.0"), "-110"),
	}

	weeks := buildHolyLockWeeks(rows, me)

	if len(weeks) != 3 {
		t.Fatalf("got %d week groups, want 3 (regular 1, regular 2, postseason 1)", len(weeks))
	}

	// Newest first, and the postseason sorts ahead of the regular season it follows.
	wantLabels := []string{"2026 · Week 1 · Postseason", "2026 · Week 2", "2026 · Week 1"}
	for i, want := range wantLabels {
		if weeks[i].Label != want {
			t.Errorf("week %d label = %q, want %q", i, weeks[i].Label, want)
		}
	}

	// Regular week 1: current user first, then case-insensitive alphabetical.
	regularWeek1 := weeks[2]
	wantOrder := []string{"Me", "Alice", "zed"}
	if len(regularWeek1.Rows) != len(wantOrder) {
		t.Fatalf("regular week 1 has %d rows, want %d", len(regularWeek1.Rows), len(wantOrder))
	}
	for i, want := range wantOrder {
		if regularWeek1.Rows[i].Username != want {
			t.Errorf("regular week 1 row %d is %q, want %q", i, regularWeek1.Rows[i].Username, want)
		}
	}
	if !regularWeek1.Rows[0].IsCurrentUser {
		t.Error("the current user's row is not flagged")
	}
	if regularWeek1.Rows[1].IsCurrentUser {
		t.Error("another member's row is flagged as the current user's")
	}

	// Each bet type renders its own pick shape.
	wantPicks := map[string]string{"Me": "GT -7", "Alice": "CLEM +150", "zed": "Over 54.5"}
	for _, entry := range regularWeek1.Rows {
		if got := entry.Pick; got != wantPicks[entry.Username] {
			t.Errorf("%s's pick = %q, want %q", entry.Username, got, wantPicks[entry.Username])
		}
		if entry.Matchup != "CLEM @ GT" {
			t.Errorf("%s's matchup = %q, want %q", entry.Username, entry.Matchup, "CLEM @ GT")
		}
	}

	// A positive spread keeps its sign, or a +3.5 dog reads as a favourite.
	if got := weeks[1].Rows[0].Pick; got != "CLEM +3.5" {
		t.Errorf("week 2 pick = %q, want %q", got, "CLEM +3.5")
	}
	if got := weeks[0].Rows[0].Pick; got != "Under 48" {
		t.Errorf("postseason pick = %q, want %q", got, "Under 48")
	}
}

func TestHolyLockWeekLabel(t *testing.T) {
	tests := []struct {
		season, week int
		seasonType   string
		want         string
	}{
		{2026, 1, "regular", "2026 · Week 1"},
		// Without the suffix this collides with the line above, and a member
		// legitimately holding both looks like a broken invariant.
		{2026, 1, "postseason", "2026 · Week 1 · Postseason"},
		{2026, -1, "regular", "2026"},
	}

	for _, tt := range tests {
		if got := holyLockWeekLabel(tt.season, tt.week, tt.seasonType); got != tt.want {
			t.Errorf("holyLockWeekLabel(%d, %d, %q) = %q, want %q", tt.season, tt.week, tt.seasonType, got, tt.want)
		}
	}
}

func TestRankLeaderboard(t *testing.T) {
	entry := func(name string, wins, losses, pushes int, balance string) LeaderboardEntry {
		return LeaderboardEntry{Username: name, Wins: wins, Losses: losses, Pushes: pushes, Balance: decimal.RequireFromString(balance)}
	}
	// Each sort puts a different member first, so a comparator that ignores
	// the requested column cannot pass all three.
	members := []LeaderboardEntry{
		entry("rich", 3, 5, 0, "1500"),    // most money, 37.5%
		entry("grinder", 6, 6, 0, "950"),  // most wins, 50%
		entry("sharp", 4, 1, 0, "1100"),   // best percentage, 80%
		entry("pusher", 0, 0, 3, "1000"),  // nothing decided: no percentage
		entry("winless", 0, 2, 0, "1000"), // 0%, which is still a percentage
	}

	tests := []struct {
		by   LeaderboardSort
		want []string
	}{
		{SortByWins, []string{"grinder", "sharp", "rich", "winless", "pusher"}},
		// 0% outranks no record at all; the pusher has not lost anything but
		// has not won anything either.
		{SortByWinPct, []string{"sharp", "grinder", "rich", "winless", "pusher"}},
		// pusher and winless tie on balance and on wins; winless has a
		// percentage, even 0%, and the pusher has none.
		{SortByBalance, []string{"rich", "sharp", "winless", "pusher", "grinder"}},
	}

	for _, tt := range tests {
		t.Run(string(tt.by), func(t *testing.T) {
			entries := slices.Clone(members)
			rankLeaderboard(entries, tt.by)

			var got []string
			for i, e := range entries {
				got = append(got, e.Username)
				if e.Rank != i+1 {
					t.Errorf("%s is at position %d but ranked %d", e.Username, i+1, e.Rank)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

// Ties on every column fall to the username, so two members level on
// everything do not swap places between page loads.
func TestRankLeaderboardBreaksFullTiesByName(t *testing.T) {
	entries := []LeaderboardEntry{
		{Username: "zed", Balance: decimal.RequireFromString("1000")},
		{Username: "Amy", Balance: decimal.RequireFromString("1000")},
		{Username: "bob", Balance: decimal.RequireFromString("1000")},
	}
	rankLeaderboard(entries, SortByWins)

	var got []string
	for _, e := range entries {
		got = append(got, e.Username)
	}
	if want := []string{"Amy", "bob", "zed"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// Percentages that are equal as fractions but not as floats must tie, which
// is why compareWinPct cross-multiplies instead of dividing.
func TestCompareWinPctIsExact(t *testing.T) {
	a := LeaderboardEntry{Wins: 1, Losses: 2}
	b := LeaderboardEntry{Wins: 3, Losses: 6}
	if n := compareWinPct(a, b); n != 0 {
		t.Errorf("1-2 vs 3-6 compared %d, want a tie", n)
	}
}

func TestLeaderboardWinPct(t *testing.T) {
	tests := []struct {
		wins, losses, pushes int
		want                 string
	}{
		{5, 3, 0, "62.5"},
		// Pushes are neither wins nor losses.
		{5, 3, 4, "62.5"},
		{2, 1, 0, "66.7"},
		{0, 4, 0, "0.0"},
		{7, 0, 0, "100.0"},
		{0, 0, 2, ""},
		{0, 0, 0, ""},
	}
	for _, tt := range tests {
		e := LeaderboardEntry{Wins: tt.wins, Losses: tt.losses, Pushes: tt.pushes}
		if got := e.WinPct(); got != tt.want {
			t.Errorf("%d-%d-%d WinPct() = %q, want %q", tt.wins, tt.losses, tt.pushes, got, tt.want)
		}
	}
}

func TestParseLeaderboardSort(t *testing.T) {
	tests := map[string]LeaderboardSort{
		"":        SortByWins,
		"wins":    SortByWins,
		"pct":     SortByWinPct,
		"balance": SortByBalance,
		"BALANCE": SortByWins,
		"rank":    SortByWins,
	}
	for in, want := range tests {
		if got := ParseLeaderboardSort(in); got != want {
			t.Errorf("ParseLeaderboardSort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateLeagueName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trims surrounding space", input: "  Sunday Money  ", want: "Sunday Money"},
		{name: "rejects empty", input: "", wantErr: ErrInvalidName},
		{name: "rejects whitespace only", input: "   \t\n ", wantErr: ErrInvalidName},
		{name: "accepts the longest name the column holds", input: strings.Repeat("a", MaxLeagueNameLength), want: strings.Repeat("a", MaxLeagueNameLength)},
		{name: "rejects one character more", input: strings.Repeat("a", MaxLeagueNameLength+1), wantErr: ErrInvalidName},
		// Postgres counts characters, not bytes, so a name of multi-byte
		// characters that fits must not be rejected for its byte length.
		{name: "counts characters not bytes", input: strings.Repeat("é", MaxLeagueNameLength), want: strings.Repeat("é", MaxLeagueNameLength)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateLeagueName(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("name = %q, want %q", got, tt.want)
			}
		})
	}
}

// membershipFixture is a public and a private league, each owned by one user,
// and a second user who belongs to neither.
type membershipFixture struct {
	t              *testing.T
	svc            *Service
	owner, alice   *models.User
	public, secret *models.League
}

func newMembershipFixture(t *testing.T) *membershipFixture {
	t.Helper()

	db := testdb.Open(t)
	f := &membershipFixture{t: t, svc: NewService(db)}
	for _, u := range []**models.User{&f.owner, &f.alice} {
		*u = &models.User{Username: "member-" + uuid.NewString()[:8], PasswordHash: "unused"}
		if err := db.Create(*u).Error; err != nil {
			t.Fatalf("creating user: %v", err)
		}
	}

	var err error
	if f.public, err = f.svc.CreateLeague("Public", f.owner.ID, true, decimal.RequireFromString("1000")); err != nil {
		t.Fatalf("creating public league: %v", err)
	}
	if f.secret, err = f.svc.CreateLeague("Secret", f.owner.ID, false, decimal.RequireFromString("500")); err != nil {
		t.Fatalf("creating private league: %v", err)
	}
	return f
}

func (f *membershipFixture) requireBalance(league *models.League, user *models.User, want string) {
	f.t.Helper()

	got, err := f.svc.GetPurseBalance(league.ID, user.ID)
	if err != nil {
		f.t.Fatalf("reading purse: %v", err)
	}
	if !got.Equal(decimal.RequireFromString(want)) {
		f.t.Errorf("balance = %s, want %s", got, want)
	}
}

func TestLeagueMembership(t *testing.T) {
	t.Run("the creator is the league's admin and holds its starting balance", func(t *testing.T) {
		f := newMembershipFixture(t)

		details, err := f.svc.GetLeagueDetails(f.secret.ID, f.owner.ID)
		if err != nil {
			t.Fatalf("GetLeagueDetails: %v", err)
		}
		if !details.IsMember || !details.IsAdmin || !details.IsCreator {
			t.Errorf("creator details = member %v, admin %v, creator %v; want all true",
				details.IsMember, details.IsAdmin, details.IsCreator)
		}
		f.requireBalance(f.secret, f.owner, "500")

		if err := f.svc.LeaveLeague(f.secret.ID, f.owner.ID); !errors.Is(err, ErrCannotLeave) {
			t.Errorf("creator leaving: error = %v, want ErrCannotLeave", err)
		}
	})

	t.Run("joining a public league opens a purse, once", func(t *testing.T) {
		f := newMembershipFixture(t)

		if err := f.svc.JoinLeague(f.public.ID, f.alice.ID); err != nil {
			t.Fatalf("joining: %v", err)
		}
		f.requireBalance(f.public, f.alice, "1000")

		if err := f.svc.JoinLeague(f.public.ID, f.alice.ID); !errors.Is(err, ErrAlreadyMember) {
			t.Errorf("joining twice: error = %v, want ErrAlreadyMember", err)
		}
	})

	t.Run("a private league is joined by its code, not by its id", func(t *testing.T) {
		f := newMembershipFixture(t)

		if err := f.svc.JoinLeague(f.secret.ID, f.alice.ID); !errors.Is(err, ErrLeagueNotPublic) {
			t.Fatalf("joining by id: error = %v, want ErrLeagueNotPublic", err)
		}
		if _, err := f.svc.JoinByCode("not-a-code", f.alice.ID); !errors.Is(err, ErrInvalidCode) {
			t.Errorf("bad code: error = %v, want ErrInvalidCode", err)
		}
		joined, err := f.svc.JoinByCode(f.secret.InviteCode, f.alice.ID)
		if err != nil {
			t.Fatalf("joining by code: %v", err)
		}
		if joined.ID != f.secret.ID {
			t.Errorf("joined %s, want %s", joined.ID, f.secret.ID)
		}
		f.requireBalance(f.secret, f.alice, "500")
	})

	t.Run("only public leagues the user is not in are offered", func(t *testing.T) {
		f := newMembershipFixture(t)

		available, err := f.svc.GetAvailableLeagues(f.alice.ID)
		if err != nil {
			t.Fatalf("GetAvailableLeagues: %v", err)
		}
		if len(available) != 1 || available[0].ID != f.public.ID {
			t.Errorf("offered %v, want only the public league", available)
		}

		available, err = f.svc.GetAvailableLeagues(f.owner.ID)
		if err != nil {
			t.Fatalf("GetAvailableLeagues: %v", err)
		}
		if len(available) != 0 {
			t.Errorf("owner offered %d leagues they are already in", len(available))
		}
	})

	// Leaving keeps the purse so a returning member comes back to the balance
	// they had -- otherwise leaving would be a way to reset a losing season.
	// Rejoining used to fail outright: the new purse collided with the old one.
	for _, rejoin := range []struct {
		name string
		join func(f *membershipFixture, league *models.League) error
	}{
		{"by id", func(f *membershipFixture, league *models.League) error {
			return f.svc.JoinLeague(league.ID, f.alice.ID)
		}},
		{"by code", func(f *membershipFixture, league *models.League) error {
			_, err := f.svc.JoinByCode(league.InviteCode, f.alice.ID)
			return err
		}},
	} {
		t.Run("a member who leaves and rejoins "+rejoin.name+" keeps their balance", func(t *testing.T) {
			f := newMembershipFixture(t)

			if err := rejoin.join(f, f.public); err != nil {
				t.Fatalf("joining: %v", err)
			}
			if err := f.svc.purseRepo.CreditWinnings(f.alice.ID, f.public.ID, decimal.RequireFromString("-750")); err != nil {
				t.Fatalf("losing money: %v", err)
			}
			if err := f.svc.LeaveLeague(f.public.ID, f.alice.ID); err != nil {
				t.Fatalf("leaving: %v", err)
			}
			if err := f.svc.LeaveLeague(f.public.ID, f.alice.ID); !errors.Is(err, ErrNotMember) {
				t.Errorf("leaving twice: error = %v, want ErrNotMember", err)
			}

			if err := rejoin.join(f, f.public); err != nil {
				t.Fatalf("rejoining: %v", err)
			}
			f.requireBalance(f.public, f.alice, "250")
		})
	}

	t.Run("only the league's admin may delete it", func(t *testing.T) {
		f := newMembershipFixture(t)
		if err := f.svc.JoinLeague(f.public.ID, f.alice.ID); err != nil {
			t.Fatalf("joining: %v", err)
		}

		if err := f.svc.DeleteLeague(f.public.ID, f.alice.ID); !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("member deleting: error = %v, want ErrNotAuthorized", err)
		}
		if err := f.svc.DeleteLeague(f.public.ID, f.owner.ID); err != nil {
			t.Fatalf("owner deleting: %v", err)
		}
		if _, err := f.svc.GetLeagueByID(f.public.ID); !errors.Is(err, ErrLeagueNotFound) {
			t.Errorf("deleted league lookup: error = %v, want ErrLeagueNotFound", err)
		}
		purses, err := f.svc.GetUserPurses(f.alice.ID)
		if err != nil {
			t.Fatalf("GetUserPurses: %v", err)
		}
		if len(purses) != 0 {
			t.Errorf("deleting the league left %d purses behind", len(purses))
		}
	})

	t.Run("only the creator may rename, and the name is validated", func(t *testing.T) {
		f := newMembershipFixture(t)
		if err := f.svc.JoinLeague(f.public.ID, f.alice.ID); err != nil {
			t.Fatalf("joining: %v", err)
		}

		if _, err := f.svc.RenameLeague(f.public.ID, f.alice.ID, "Mine now"); !errors.Is(err, ErrNotAuthorized) {
			t.Errorf("member renaming: error = %v, want ErrNotAuthorized", err)
		}
		if _, err := f.svc.RenameLeague(f.public.ID, f.owner.ID, "   "); !errors.Is(err, ErrInvalidName) {
			t.Errorf("blank name: error = %v, want ErrInvalidName", err)
		}
		if _, err := f.svc.RenameLeague(f.public.ID, f.owner.ID, "  Sunday Club  "); err != nil {
			t.Fatalf("renaming: %v", err)
		}
		league, err := f.svc.GetLeagueByID(f.public.ID)
		if err != nil {
			t.Fatalf("GetLeagueByID: %v", err)
		}
		if league.Name != "Sunday Club" {
			t.Errorf("name = %q, want the trimmed %q", league.Name, "Sunday Club")
		}
	})
}
