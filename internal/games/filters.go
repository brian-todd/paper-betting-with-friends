package games

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/brian/paper-betting-with-friends/internal/repository"
)

// appliedParam marks a request as carrying a filter the user actually chose.
//
// The controls are checkboxes, so "nothing selected" and "never touched"
// both arrive as an absent parameter. Without this marker, clearing the
// division filter would silently snap back to the default instead of showing
// every game.
const appliedParam = "applied"

// defaultFilter is what /games narrows to before anyone touches a filter:
// bettable FBS games.
//
// FBS is the ten major conferences plus the FBS independents, which is both the
// set people mean by "major conferences" and the tier the odds feed covers
// completely, while DII and DIII carry no lines at all. Filtering on the
// classification rather than a hardcoded list of conference names means
// realignment is the data feed's problem, not ours.
//
// Bettable-only costs nothing on a normal week, since every FBS game has a
// line. It earns its place when the odds feed is behind: a card with no line
// is one you cannot act on, and burying the ones you can under it is the
// opposite of what a landing page should do.
func defaultFilter() Filter {
	return Filter{
		Tiers:     []string{"fbs"},
		Bettable:  true,
		Defaulted: true,
	}
}

// tierLabels maps the API's classification codes to what the filter shows.
// Order is the display order, strongest division first.
var tierLabels = []struct {
	Code  string
	Label string
}{
	{"fbs", "FBS"},
	{"fcs", "FCS"},
	{"ii", "Division II"},
	{"iii", "Division III"},
}

// statusOptions are the game states the filter offers, in display order.
var statusOptions = []struct {
	Value string
	Label string
}{
	{"", "Any status"},
	{"in_progress", "Live"},
	{"scheduled", "Scheduled"},
	{"final", "Final"},
	{"postponed", "Postponed"},
	{"cancelled", "Cancelled"},
}

// gameTypeOptions are the conference-vs-non-conference choices the filter
// offers, in display order. Conference and non-conference are mutually
// exclusive states of one field, so this is a select like status rather than
// two independent checkboxes -- there is no sane meaning for checking both.
var gameTypeOptions = []struct {
	Value string
	Label string
}{
	{"", "Any matchup"},
	{"conference", "Conference"},
	{"nonconference", "Non-conference"},
}

// weekdayOptions are the kickoff days the filter offers. Values match
// Postgres EXTRACT(DOW), which counts from Sunday. College football runs
// Tuesday through Monday in practice, but the full week is offered so a
// midweek MAC game is still reachable.
var weekdayOptions = []struct {
	Value int
	Label string
}{
	{0, "Sun"},
	{1, "Mon"},
	{2, "Tue"},
	{3, "Wed"},
	{4, "Thu"},
	{5, "Fri"},
	{6, "Sat"},
}

// Filter is the parsed state of the games filter. It carries both what the
// repository needs to narrow the query and what the template needs to re-render
// the controls in the state the user left them.
type Filter struct {
	Tiers       []string
	Conferences []string
	Status      string
	Team        string
	Bettable    bool
	Weekdays    []int
	FromHour    *int
	ToHour      *int

	// Odds ranges, kept as decimals so a 2.5 spread compares exactly.
	SpreadMin    *decimal.Decimal
	SpreadMax    *decimal.Decimal
	TotalMin     *decimal.Decimal
	TotalMax     *decimal.Decimal
	MoneyLineMin *decimal.Decimal
	MoneyLineMax *decimal.Decimal

	// RankedTeam and RankedMatchup narrow on poll position within the week
	// being viewed. Checking both normalises to RankedMatchup alone, the
	// stricter of the two -- see ParseFilter.
	RankedTeam    bool
	RankedMatchup bool

	// NeutralSite narrows to games at a neutral site. GameType is "",
	// "conference" or "nonconference" -- see gameTypeOptions.
	NeutralSite bool
	GameType    string

	// Defaulted reports that no filter was submitted, so the narrowing came
	// from defaultFilter rather than from the user.
	Defaulted bool
}

// ParseFilter reads the games filter out of a URL query.
//
// Unparseable values are dropped rather than rejected: a filter is a view
// preference, and a mangled bookmark should show games, not an error page.
func ParseFilter(query url.Values) Filter {
	filter := Filter{
		Tiers:       cleanValues(query["tier"]),
		Conferences: cleanValues(query["conf"]),
		Status:      strings.TrimSpace(query.Get("status")),
		Team:        strings.TrimSpace(query.Get("team")),
		Bettable:    query.Get("bettable") == "1",
		Weekdays:    parseWeekdays(query["day"]),
		FromHour:    parseHour(query.Get("from")),
		ToHour:      parseHour(query.Get("to")),

		SpreadMin:    parseDecimal(query.Get("spread_min")),
		SpreadMax:    parseDecimal(query.Get("spread_max")),
		TotalMin:     parseDecimal(query.Get("total_min")),
		TotalMax:     parseDecimal(query.Get("total_max")),
		MoneyLineMin: parseDecimal(query.Get("ml_min")),
		MoneyLineMax: parseDecimal(query.Get("ml_max")),

		RankedTeam:    query.Get("ranked") == "1",
		RankedMatchup: query.Get("ranked_matchup") == "1",

		NeutralSite: query.Get("neutral") == "1",
		GameType:    strings.TrimSpace(query.Get("game_type")),
	}

	if !validStatus(filter.Status) {
		filter.Status = ""
	}
	if !validGameType(filter.GameType) {
		filter.GameType = ""
	}

	// Both checked normalises to the stricter one, so one meaning reaches the
	// rest of the stack and Query() round-trips.
	if filter.RankedMatchup {
		filter.RankedTeam = false
	}

	// An hour range entered backwards would match nothing at all, which reads
	// as a broken page rather than an empty one. Swapping is what the user
	// meant.
	if filter.FromHour != nil && filter.ToHour != nil && *filter.FromHour > *filter.ToHour {
		filter.FromHour, filter.ToHour = filter.ToHour, filter.FromHour
	}
	swapIfReversed(&filter.SpreadMin, &filter.SpreadMax)
	swapIfReversed(&filter.TotalMin, &filter.TotalMax)
	swapIfReversed(&filter.MoneyLineMin, &filter.MoneyLineMax)

	if query.Get(appliedParam) != "1" && filter.isEmpty() {
		filter = defaultFilter()
	}

	return filter
}

// isEmpty reports whether nothing at all was selected.
func (f Filter) isEmpty() bool {
	return len(f.Tiers) == 0 && len(f.Conferences) == 0 && f.Status == "" &&
		f.Team == "" && !f.Bettable && len(f.Weekdays) == 0 &&
		f.FromHour == nil && f.ToHour == nil &&
		f.SpreadMin == nil && f.SpreadMax == nil &&
		f.TotalMin == nil && f.TotalMax == nil &&
		f.MoneyLineMin == nil && f.MoneyLineMax == nil &&
		!f.RankedTeam && !f.RankedMatchup &&
		!f.NeutralSite && f.GameType == ""
}

// Active reports whether the filter narrows anything, which is what decides
// if the page offers a "clear filters" affordance.
func (f Filter) Active() bool { return !f.isEmpty() }

// Repository converts the filter into the repository's query shape. The
// location is filled in by the service, which owns it.
func (f Filter) Repository() repository.GameFilter {
	return repository.GameFilter{
		Conferences:     f.Conferences,
		Tiers:           f.Tiers,
		Status:          f.Status,
		Team:            f.Team,
		BettableOnly:    f.Bettable,
		Weekdays:        f.Weekdays,
		StartHour:       f.FromHour,
		EndHour:         f.ToHour,
		SpreadMin:       f.SpreadMin,
		SpreadMax:       f.SpreadMax,
		TotalMin:        f.TotalMin,
		TotalMax:        f.TotalMax,
		MoneyLineMin:    f.MoneyLineMin,
		MoneyLineMax:    f.MoneyLineMax,
		RankedTeam:      f.RankedTeam,
		RankedMatchup:   f.RankedMatchup,
		NeutralSiteOnly: f.NeutralSite,
		ConferenceGame:  f.conferenceGameFilter(),
	}
}

// conferenceGameFilter translates GameType into the repository's tri-state
// bool -- nil for "any", so the zero-value Filter still matches every game.
func (f Filter) conferenceGameFilter() *bool {
	switch f.GameType {
	case "conference":
		v := true
		return &v
	case "nonconference":
		v := false
		return &v
	default:
		return nil
	}
}

// Query re-encodes the filter for links that must survive it, such as the
// pagination controls. The page number is deliberately excluded so callers
// append their own.
func (f Filter) Query() url.Values {
	query := url.Values{}
	// A defaulted filter is not something the user chose, and a bare URL
	// already means it. Serialising it would both dirty every link and freeze
	// the default in, so "clear" could never widen past FBS.
	if f.Defaulted {
		return query
	}
	for _, tier := range f.Tiers {
		query.Add("tier", tier)
	}
	for _, conference := range f.Conferences {
		query.Add("conf", conference)
	}
	if f.Status != "" {
		query.Set("status", f.Status)
	}
	if f.Team != "" {
		query.Set("team", f.Team)
	}
	if f.Bettable {
		query.Set("bettable", "1")
	}
	for _, day := range f.Weekdays {
		query.Add("day", strconv.Itoa(day))
	}
	if f.FromHour != nil {
		query.Set("from", strconv.Itoa(*f.FromHour))
	}
	if f.ToHour != nil {
		query.Set("to", strconv.Itoa(*f.ToHour))
	}
	setDecimal(query, "spread_min", f.SpreadMin)
	setDecimal(query, "spread_max", f.SpreadMax)
	setDecimal(query, "total_min", f.TotalMin)
	setDecimal(query, "total_max", f.TotalMax)
	setDecimal(query, "ml_min", f.MoneyLineMin)
	setDecimal(query, "ml_max", f.MoneyLineMax)
	if f.RankedTeam {
		query.Set("ranked", "1")
	}
	if f.RankedMatchup {
		query.Set("ranked_matchup", "1")
	}
	if f.NeutralSite {
		query.Set("neutral", "1")
	}
	if f.GameType != "" {
		query.Set("game_type", f.GameType)
	}
	query.Set(appliedParam, "1")
	return query
}

// SelectedTiers indexes the chosen tiers for the template's checkbox state.
func (f Filter) SelectedTiers() map[string]bool { return toSet(f.Tiers) }

// SelectedConferences indexes the chosen conferences for the template.
func (f Filter) SelectedConferences() map[string]bool { return toSet(f.Conferences) }

// SelectedWeekdays indexes the chosen kickoff days for the template.
func (f Filter) SelectedWeekdays() map[int]bool {
	set := make(map[int]bool, len(f.Weekdays))
	for _, day := range f.Weekdays {
		set[day] = true
	}
	return set
}

// ConferenceGroup is a division's conferences, so the filter can present 70-odd
// conference names as a handful of collapsible groups.
type ConferenceGroup struct {
	Tier        string
	Label       string
	Conferences []repository.WeekConference
}

// GroupConferences buckets a week's conferences by division, in tier order.
// Conferences with a classification the labels do not cover are collected under
// "Other" rather than dropped, so no game becomes unreachable.
func GroupConferences(conferences []repository.WeekConference) []ConferenceGroup {
	byTier := make(map[string][]repository.WeekConference)
	for _, conference := range conferences {
		byTier[conference.Classification] = append(byTier[conference.Classification], conference)
	}

	groups := make([]ConferenceGroup, 0, len(tierLabels)+1)
	for _, tier := range tierLabels {
		if rows := byTier[tier.Code]; len(rows) > 0 {
			groups = append(groups, ConferenceGroup{Tier: tier.Code, Label: tier.Label, Conferences: rows})
			delete(byTier, tier.Code)
		}
	}

	var other []repository.WeekConference
	for _, rows := range byTier {
		other = append(other, rows...)
	}
	if len(other) > 0 {
		groups = append(groups, ConferenceGroup{Tier: "", Label: "Other", Conferences: other})
	}
	return groups
}

// HourOption is one entry in the kickoff-window selects.
type HourOption struct {
	Value int
	Label string
}

// HourOptions lists the 24 hours of the day as the kickoff window offers them.
//
// These are wall-clock hours in the app's configured timezone, resolved on the
// server. Unlike a kickoff instant they are not converted per reader -- "games
// after 7pm" has to mean one thing for everyone, or two people sharing a link
// would see different games.
func HourOptions() []HourOption {
	options := make([]HourOption, 0, 24)
	for hour := range 24 {
		options = append(options, HourOption{Value: hour, Label: hourLabel(hour)})
	}
	return options
}

// hourLabel renders an hour as a 12-hour clock reading.
func hourLabel(hour int) string {
	switch {
	case hour == 0:
		return "12 AM"
	case hour < 12:
		return fmt.Sprintf("%d AM", hour)
	case hour == 12:
		return "12 PM"
	default:
		return fmt.Sprintf("%d PM", hour-12)
	}
}

// ZoneAbbreviation names the timezone the kickoff window is expressed in, so
// the label can say which clock it means.
func ZoneAbbreviation(location *time.Location) string {
	if location == nil {
		return "UTC"
	}
	return time.Now().In(location).Format("MST")
}

// TierOptions exposes the division list to the template.
func TierOptions() []struct {
	Code  string
	Label string
} {
	return tierLabels
}

// StatusOptions exposes the status list to the template.
func StatusOptions() []struct {
	Value string
	Label string
} {
	return statusOptions
}

// GameTypeOptions exposes the conference/non-conference choices to the template.
func GameTypeOptions() []struct {
	Value string
	Label string
} {
	return gameTypeOptions
}

// WeekdayOptions exposes the kickoff-day list to the template.
func WeekdayOptions() []struct {
	Value int
	Label string
} {
	return weekdayOptions
}

// cleanValues trims and drops blanks, which is what an unchecked box or a
// hand-edited URL leaves behind.
func cleanValues(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

// parseWeekdays keeps only values Postgres EXTRACT(DOW) can produce.
func parseWeekdays(values []string) []int {
	days := make([]int, 0, len(values))
	for _, value := range values {
		day, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || day < 0 || day > 6 {
			continue
		}
		days = append(days, day)
	}
	if len(days) == 0 {
		return nil
	}
	return days
}

// parseHour accepts an hour of the day, returning nil for anything else.
func parseHour(value string) *int {
	hour, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || hour < 0 || hour > 23 {
		return nil
	}
	return &hour
}

// validStatus reports whether a status is one the filter offers.
func validStatus(status string) bool {
	for _, option := range statusOptions {
		if option.Value == status {
			return true
		}
	}
	return false
}

// validGameType reports whether a value is one the filter offers.
func validGameType(gameType string) bool {
	for _, option := range gameTypeOptions {
		if option.Value == gameType {
			return true
		}
	}
	return false
}

// toSet indexes a slice for template lookups.
func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

// FromValue and ToValue expose the kickoff window as plain ints for the
// template, which cannot compare a *int against an option value. -1 means
// unset.
func (f Filter) FromValue() int { return hourValue(f.FromHour) }

// ToValue is the end of the kickoff window, or -1 when unset.
func (f Filter) ToValue() int { return hourValue(f.ToHour) }

// SpreadMinValue and the rest expose the odds bounds as strings, since a
// template cannot render a nil *decimal.Decimal as an empty input.
func (f Filter) SpreadMinValue() string { return decimalValue(f.SpreadMin) }

// SpreadMaxValue is the upper spread bound, empty when unset.
func (f Filter) SpreadMaxValue() string { return decimalValue(f.SpreadMax) }

// TotalMinValue is the lower over/under bound, empty when unset.
func (f Filter) TotalMinValue() string { return decimalValue(f.TotalMin) }

// TotalMaxValue is the upper over/under bound, empty when unset.
func (f Filter) TotalMaxValue() string { return decimalValue(f.TotalMax) }

// MoneyLineMinValue is the lower money line bound, empty when unset.
func (f Filter) MoneyLineMinValue() string { return decimalValue(f.MoneyLineMin) }

// MoneyLineMaxValue is the upper money line bound, empty when unset.
func (f Filter) MoneyLineMaxValue() string { return decimalValue(f.MoneyLineMax) }

func decimalValue(value *decimal.Decimal) string {
	if value == nil {
		return ""
	}
	return value.String()
}

// parseDecimal accepts a number, returning nil for a blank or malformed one.
func parseDecimal(value string) *decimal.Decimal {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parsed, err := decimal.NewFromString(trimmed)
	if err != nil {
		return nil
	}
	return &parsed
}

// setDecimal adds a bound to a query only when it is set.
func setDecimal(query url.Values, key string, value *decimal.Decimal) {
	if value != nil {
		query.Set(key, value.String())
	}
}

// swapIfReversed puts a backwards range the right way round. Left alone it
// would match nothing, which reads as a broken page rather than an empty one.
func swapIfReversed(low, high **decimal.Decimal) {
	if *low != nil && *high != nil && (*low).GreaterThan(**high) {
		*low, *high = *high, *low
	}
}

func hourValue(hour *int) int {
	if hour == nil {
		return -1
	}
	return *hour
}
