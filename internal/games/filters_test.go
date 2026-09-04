package games

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/repository"
	"github.com/shopspring/decimal"
)

func TestParseFilter(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  Filter
	}{
		{
			// A bare visit gets the bettable-FBS default, which is what keeps a
			// 412-game week down to something worth rendering.
			name:  "no query defaults to bettable FBS",
			query: "",
			want:  Filter{Tiers: []string{"fbs"}, Bettable: true, Defaulted: true},
		},
		{
			// The whole point of the applied marker: an empty form is a
			// deliberate "show me everything", not an untouched page.
			name:  "applied with nothing selected clears the default",
			query: "applied=1",
			want:  Filter{},
		},
		{
			name:  "an explicit filter suppresses the default",
			query: "tier=fcs&tier=ii",
			want:  Filter{Tiers: []string{"fcs", "ii"}},
		},
		{
			name:  "conferences alone suppress the tier default",
			query: "conf=SEC&conf=Big+Ten",
			want:  Filter{Conferences: []string{"SEC", "Big Ten"}},
		},
		{
			name: "full filter",
			query: "applied=1&tier=fbs&conf=SEC&status=final&team=bama&bettable=1&day=6&day=4&from=12&to=20" +
				"&spread_min=3.5&spread_max=14&total_min=45.5&total_max=60&ml_min=-250&ml_max=300&ranked=1",
			want: Filter{
				Tiers:        []string{"fbs"},
				Conferences:  []string{"SEC"},
				Status:       "final",
				Team:         "bama",
				Bettable:     true,
				Weekdays:     []int{6, 4},
				FromHour:     new(12),
				ToHour:       new(20),
				SpreadMin:    decPtr("3.5"),
				SpreadMax:    decPtr("14"),
				TotalMin:     decPtr("45.5"),
				TotalMax:     decPtr("60"),
				MoneyLineMin: decPtr("-250"),
				MoneyLineMax: decPtr("300"),
				RankedTeam:   true,
			},
		},
		{
			name:  "ranked matchup alone",
			query: "applied=1&ranked_matchup=1",
			want:  Filter{RankedMatchup: true},
		},
		{
			// The stricter filter wins, so one meaning reaches the rest of the
			// stack and Query() round-trips.
			name:  "both ranked boxes checked collapses to matchup only",
			query: "applied=1&ranked=1&ranked_matchup=1",
			want:  Filter{RankedMatchup: true},
		},
		{
			// Backwards, these would match nothing at all.
			name:  "reversed odds ranges are swapped",
			query: "applied=1&spread_min=14&spread_max=3.5&ml_min=300&ml_max=-250",
			want: Filter{
				SpreadMin:    decPtr("3.5"),
				SpreadMax:    decPtr("14"),
				MoneyLineMin: decPtr("-250"),
				MoneyLineMax: decPtr("300"),
			},
		},
		{
			// A mangled bookmark should show games, not an error page.
			name:  "junk values are dropped, not rejected",
			query: "applied=1&status=exploded&day=9&day=-1&day=cat&from=99&to=abc&conf=+&tier=&spread_min=lots&total_max=",
			want:  Filter{},
		},
		{
			// Backwards, this would match nothing and read as a broken page.
			name:  "a reversed hour window is swapped",
			query: "applied=1&from=20&to=8",
			want:  Filter{FromHour: new(8), ToHour: new(20)},
		},
		{
			name:  "whitespace is trimmed",
			query: "applied=1&team=++Ohio+State++",
			want:  Filter{Team: "Ohio State"},
		},
		{
			name:  "an empty status is a valid any-status choice",
			query: "applied=1&status=",
			want:  Filter{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("ParseQuery(%q): %v", tt.query, err)
			}

			got := ParseFilter(query)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseFilter(%q)\n got %+v\nwant %+v", tt.query, got, tt.want)
			}
		})
	}
}

func TestFilterQueryRoundTrips(t *testing.T) {
	original := Filter{
		Tiers:         []string{"fbs", "fcs"},
		Conferences:   []string{"SEC", "Big Ten"},
		Status:        "scheduled",
		Team:          "Ohio State",
		Bettable:      true,
		Weekdays:      []int{4, 6},
		FromHour:      new(9),
		ToHour:        new(23),
		SpreadMin:     decPtr("2.5"),
		TotalMax:      decPtr("58.5"),
		MoneyLineMin:  decPtr("-150"),
		RankedMatchup: true,
	}

	// Pagination links are built from Query(), so anything it drops is a filter
	// that silently widens when the user turns the page.
	reparsed := ParseFilter(original.Query())
	if !reflect.DeepEqual(reparsed, original) {
		t.Errorf("round trip\n got %+v\nwant %+v", reparsed, original)
	}
}

func TestFilterQueryOmitsAppliedWhenDefaulted(t *testing.T) {
	defaulted := ParseFilter(url.Values{})
	if !defaulted.Defaulted {
		t.Fatal("expected a bare query to be defaulted")
	}

	// Stamping the default as applied would freeze it into every later link,
	// so "clear" could never widen past FBS.
	if got := defaulted.Query().Get(appliedParam); got != "" {
		t.Errorf("Query() set %s=%q on a defaulted filter, want it absent", appliedParam, got)
	}
	if got := ParseFilter(defaulted.Query()); !got.Defaulted {
		t.Error("a defaulted filter should survive a round trip as defaulted")
	}
}

func TestFilterActive(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{"untouched default counts as active", "", true},
		{"explicitly cleared", "applied=1", false},
		{"team search only", "applied=1&team=duke", true},
		{"bettable only", "applied=1&bettable=1", true},
		{"odds range only", "applied=1&total_min=50", true},
		{"ranked team only", "applied=1&ranked=1", true},
		{"ranked matchup only", "applied=1&ranked_matchup=1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("ParseQuery(%q): %v", tt.query, err)
			}
			if got := ParseFilter(query).Active(); got != tt.want {
				t.Errorf("Active() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterRepository(t *testing.T) {
	filter := ParseFilter(url.Values{
		"applied":  {"1"},
		"tier":     {"fbs"},
		"bettable": {"1"},
		"from":     {"18"},
		"ranked":   {"1"},
	})

	got := filter.Repository()
	want := repository.GameFilter{
		Tiers:        []string{"fbs"},
		BettableOnly: true,
		StartHour:    new(18),
		RankedTeam:   true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Repository()\n got %+v\nwant %+v", got, want)
	}
}

func TestGroupConferences(t *testing.T) {
	conferences := []repository.WeekConference{
		{Conference: "SEC", Classification: "fbs", Games: 8},
		{Conference: "Ivy", Classification: "fcs", Games: 4},
		{Conference: "NESCAC", Classification: "iii", Games: 5},
		{Conference: "Lone Star", Classification: "ii", Games: 6},
		// A classification the labels do not cover must not vanish, or its
		// games become unreachable through the filter.
		{Conference: "Some New Tier", Classification: "naia", Games: 2},
	}

	groups := GroupConferences(conferences)

	wantLabels := []string{"FBS", "FCS", "Division II", "Division III", "Other"}
	gotLabels := make([]string, len(groups))
	for i, group := range groups {
		gotLabels[i] = group.Label
	}
	if !reflect.DeepEqual(gotLabels, wantLabels) {
		t.Fatalf("group order = %v, want %v", gotLabels, wantLabels)
	}

	if got := groups[4].Conferences[0].Conference; got != "Some New Tier" {
		t.Errorf("unknown tier landed on %q, want it under Other", got)
	}
}

func TestGroupConferencesSkipsEmptyTiers(t *testing.T) {
	groups := GroupConferences([]repository.WeekConference{
		{Conference: "SEC", Classification: "fbs", Games: 8},
	})
	if len(groups) != 1 || groups[0].Label != "FBS" {
		t.Errorf("got %d groups (%+v), want only FBS", len(groups), groups)
	}
}

func TestPaginate(t *testing.T) {
	tests := []struct {
		name  string
		page  int
		total int
		want  Page
	}{
		{
			name:  "first page of many",
			page:  1,
			total: 412,
			want:  Page{Number: 1, Size: 100, Total: 412, Pages: 5, First: 1, Last: 100},
		},
		{
			// The last page is short, so Last must be the total, not the slot.
			name:  "short final page",
			page:  5,
			total: 412,
			want:  Page{Number: 5, Size: 100, Total: 412, Pages: 5, First: 401, Last: 412},
		},
		{
			// A bookmark from a wider filter: clamp rather than show nothing.
			name:  "page past the end clamps to the last",
			page:  99,
			total: 150,
			want:  Page{Number: 2, Size: 100, Total: 150, Pages: 2, First: 101, Last: 150},
		},
		{
			name:  "page below one clamps to the first",
			page:  -3,
			total: 150,
			want:  Page{Number: 1, Size: 100, Total: 150, Pages: 2, First: 1, Last: 100},
		},
		{
			name:  "no results still has one page",
			page:  1,
			total: 0,
			want:  Page{Number: 1, Size: 100, Total: 0, Pages: 1, First: 0, Last: 0},
		},
		{
			name:  "exactly one full page",
			page:  1,
			total: 100,
			want:  Page{Number: 1, Size: 100, Total: 100, Pages: 1, First: 1, Last: 100},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := paginate(tt.page, tt.total)
			if got != tt.want {
				t.Errorf("paginate(%d, %d)\n got %+v\nwant %+v", tt.page, tt.total, got, tt.want)
			}
		})
	}
}

func TestPageNavigation(t *testing.T) {
	middle := paginate(3, 412)
	if !middle.HasPrev() || !middle.HasNext() {
		t.Error("a middle page should have both neighbours")
	}
	if middle.Prev() != 2 || middle.Next() != 4 {
		t.Errorf("Prev/Next = %d/%d, want 2/4", middle.Prev(), middle.Next())
	}

	// The disabled controls still render an href, so the ends must not run off.
	first := paginate(1, 412)
	if first.HasPrev() || first.Prev() != 1 {
		t.Errorf("first page: HasPrev=%v Prev=%d, want false/1", first.HasPrev(), first.Prev())
	}
	last := paginate(5, 412)
	if last.HasNext() || last.Next() != 5 {
		t.Errorf("last page: HasNext=%v Next=%d, want false/5", last.HasNext(), last.Next())
	}
}

func TestPageURL(t *testing.T) {
	const weekPath = "/games/2026/regular/1"

	tests := []struct {
		name  string
		query string
		page  int
		want  string
	}{
		{
			// Page 1 is the canonical URL, so it carries no page parameter.
			name:  "defaulted filter on page one is the bare path",
			query: "",
			page:  1,
			want:  weekPath,
		},
		{
			name:  "the filter rides along to other pages",
			query: "applied=1&tier=fbs&team=duke",
			page:  3,
			want:  weekPath + "?applied=1&page=3&team=duke&tier=fbs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("ParseQuery(%q): %v", tt.query, err)
			}
			if got := pageURL(weekPath, ParseFilter(query), tt.page); got != tt.want {
				t.Errorf("pageURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

//go:fix inline
func intPtr(v int) *int { return new(v) }

func decPtr(v string) *decimal.Decimal {
	d := decimal.RequireFromString(v)
	return &d
}
