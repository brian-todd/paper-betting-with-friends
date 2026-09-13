package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// TeamATSRecord is a team's current record against the spread.
//
// It is the one team-season resource in this group that is about betting rather
// than about football: a rating says who is better, this says who has been
// beating the number.
type TeamATSRecord struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	TeamID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_team_ats_records_team_season"`
	Season int       `gorm:"not null;uniqueIndex:idx_team_ats_records_team_season"`

	// FetchedAt drives the panel's "as of" line. See TeamRating.FetchedAt.
	FetchedAt time.Time `gorm:"not null"`

	Games     int `gorm:"not null;default:0"`
	ATSWins   int `gorm:"not null;default:0"`
	ATSLosses int `gorm:"not null;default:0"`
	ATSPushes int `gorm:"not null;default:0"`

	// AvgCoverMargin is points relative to the spread, averaged over the
	// season: positive means the team has beaten the number by that much on
	// average. Nil for a team with no graded games.
	AvgCoverMargin *decimal.Decimal `gorm:"type:decimal(6,2)"`

	CreatedAt time.Time
	UpdatedAt time.Time

	// Relationships.
	Team *Team `gorm:"foreignKey:TeamID"`
}

// Record is the ATS line as a won-lost-tied split, so it renders through the
// same helper a straight-up record does.
//
// A push occupies the Ties slot, which is what it is: the bettor got their
// stake back. That makes "6-4-1" mean the same shape of thing in both rows of
// the panel.
func (r *TeamATSRecord) Record() WinLoss {
	if r == nil {
		return WinLoss{}
	}
	return WinLoss{Games: r.Games, Wins: r.ATSWins, Losses: r.ATSLosses, Ties: r.ATSPushes}
}

// CoverMarginText renders the average cover margin with an explicit sign, since
// the number is meaningless without one -- "3.5" could be either side of the
// spread. Empty when there is none.
func (r *TeamATSRecord) CoverMarginText() string {
	if r == nil || r.AvgCoverMargin == nil {
		return ""
	}

	margin := r.AvgCoverMargin.Round(1)
	if margin.IsNegative() {
		return margin.String()
	}
	return "+" + margin.String()
}

// BeforeCreate sets the UUID before creating a new ATS record.
func (r *TeamATSRecord) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}
