package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TeamBox is what a team did, as counts: over a season in TeamSeasonStats and over one
// game in GameTeamStats. Sharing the type is what keeps the game page's rows the same
// before and after tip-off. A nil count is one the feed did not report.
type TeamBox struct {
	Points             *int `gorm:"column:points"`
	FGMade             *int `gorm:"column:fg_made"`
	FGAttempted        *int `gorm:"column:fg_attempted"`
	ThreeMade          *int `gorm:"column:three_made"`
	ThreeAttempted     *int `gorm:"column:three_attempted"`
	FTMade             *int `gorm:"column:ft_made"`
	FTAttempted        *int `gorm:"column:ft_attempted"`
	Rebounds           *int `gorm:"column:rebounds"`
	OffRebounds        *int `gorm:"column:off_rebounds"`
	DefRebounds        *int `gorm:"column:def_rebounds"`
	Assists            *int `gorm:"column:assists"`
	Steals             *int `gorm:"column:steals"`
	Blocks             *int `gorm:"column:blocks"`
	Turnovers          *int `gorm:"column:turnovers"`
	Fouls              *int `gorm:"column:fouls"`
	PaintPoints        *int `gorm:"column:paint_points"`
	FastBreakPoints    *int `gorm:"column:fast_break_points"`
	PointsOffTurnovers *int `gorm:"column:points_off_turnovers"`
}

// TeamBoxColumns are TeamBox's columns, for an upsert to assign.
var TeamBoxColumns = []string{
	"points", "fg_made", "fg_attempted", "three_made", "three_attempted",
	"ft_made", "ft_attempted", "rebounds", "off_rebounds", "def_rebounds",
	"assists", "steals", "blocks", "turnovers", "fouls",
	"paint_points", "fast_break_points", "points_off_turnovers",
}

// Reported says whether the feed gave any figures. Points is the test because a team
// that played has some, and a season row written before these columns existed has none.
func (b TeamBox) Reported() bool {
	return b.Points != nil
}

// GameTeamStats is one team's box score in one finished basketball game.
type GameTeamStats struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	GameID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:game_team_stats_game_id_team_id_key"`
	TeamID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:game_team_stats_game_id_team_id_key"`
	FetchedAt time.Time `gorm:"not null"`

	Box TeamBox `gorm:"embedded"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName keeps GORM from pluralizing a name that is already plural.
func (GameTeamStats) TableName() string { return "game_team_stats" }

// BeforeCreate sets the UUID before creating a new row.
func (s *GameTeamStats) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
