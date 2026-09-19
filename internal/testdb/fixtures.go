package testdb

import (
	"strings"
	"testing"
	"time"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// defaultSeason is the year fixtures land in when a test does not care. Games
// are found by their own attributes here, never by the season, so the value
// only has to be a value.
const defaultSeason = 2026

// InsertVenue writes a venue, which every team and game needs a foreign key to.
func InsertVenue(t *testing.T, db *gorm.DB) *models.Venue {
	t.Helper()

	venue := &models.Venue{
		Name:     "Test Venue " + unique(),
		City:     "Testville",
		State:    "TS",
		Capacity: 50000,
	}
	if err := db.Create(venue).Error; err != nil {
		t.Fatalf("inserting venue: %v", err)
	}

	return venue
}

// InsertTeam writes a football team in the given division, along with the venue
// it plays at.
//
// The classification is the only field a caller is ever likely to care about:
// it is what the scoreboard's schedule queries scope on, and it is stored in
// the case CFBD reports rather than the case an operator might configure.
func InsertTeam(t *testing.T, db *gorm.DB, classification string) *models.Team {
	t.Helper()

	venue := InsertVenue(t, db)
	suffix := unique()
	team := &models.Team{
		Sport: models.SportFootball,
		Name:  "Test Team " + suffix,
		// Unique per sport, and only ten characters wide.
		Abbreviation:   suffix,
		Conference:     "Test Conference",
		Classification: &classification,
		HomeVenueID:    venue.ID,
	}
	if err := db.Create(team).Error; err != nil {
		t.Fatalf("inserting team: %v", err)
	}

	return team
}

// InsertGame writes a game, filling in anything the test left zero.
//
// A caller sets the fields its assertion turns on -- usually the kickoff, the
// status and the home team's division -- and is spared building the other side,
// the venue and the season every time.
func InsertGame(t *testing.T, db *gorm.DB, game models.Game) *models.Game {
	t.Helper()

	if game.Sport == "" {
		game.Sport = models.SportFootball
	}
	if game.Status == "" {
		game.Status = models.GameStatusScheduled
	}
	if game.Season == 0 {
		game.Season = defaultSeason
	}
	if game.ScheduledAt.IsZero() {
		game.ScheduledAt = time.Now()
	}
	if game.HomeTeamID == uuid.Nil {
		game.HomeTeamID = InsertTeam(t, db, "fbs").ID
	}
	if game.AwayTeamID == uuid.Nil {
		game.AwayTeamID = InsertTeam(t, db, "fbs").ID
	}
	if game.VenueID == uuid.Nil {
		game.VenueID = InsertVenue(t, db).ID
	}

	if err := db.Create(&game).Error; err != nil {
		t.Fatalf("inserting game: %v", err)
	}

	return &game
}

// unique returns a short token distinct across processes.
//
// Fixtures roll back, but a unique index still sees an uncommitted row from
// another transaction and blocks on it, so two packages running in parallel
// would serialize on a shared abbreviation rather than fail on it -- slower and
// far stranger to diagnose than simply not colliding.
func unique() string {
	return strings.ToUpper(uuid.NewString()[:8])
}
