package repository

import (
	"github.com/brian/paper-betting-with-friends/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RankingRepository provides methods for interacting with team poll rankings.
type RankingRepository struct {
	db *gorm.DB
}

// NewRankingRepository creates a new RankingRepository.
func NewRankingRepository(db *gorm.DB) *RankingRepository {
	return &RankingRepository{db: db}
}

// ReplaceWeekPoll swaps a week's rows for one poll: everything currently
// stored for (weekID, poll) is deleted and rankings is inserted in its place,
// inside one transaction. A plain upsert would leave a team ranked forever
// after it drops out of the poll, since nothing would ever remove its row.
func (r *RankingRepository) ReplaceWeekPoll(weekID uuid.UUID, poll string, rankings []models.TeamRanking) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("week_id = ? AND poll = ?", weekID, poll).Delete(&models.TeamRanking{}).Error; err != nil {
			return err
		}
		if len(rankings) == 0 {
			return nil
		}
		return tx.Create(&rankings).Error
	})
}

// EffectiveRanks resolves which poll counts as "ranked" for a week -- the CFP
// committee rankings when the week has them, else AP Top 25 -- and returns
// that poll's name alongside a team ID -> rank map. This is the single place
// the "CFP when available, else AP" rule lives; every other reader takes its
// answer rather than re-deciding.
//
// ("", nil, nil) means the week has rows in neither poll, which is the honest
// answer for a preseason or bye week: no game in it is a ranked matchup.
func (r *RankingRepository) EffectiveRanks(weekID uuid.UUID) (string, map[uuid.UUID]int, error) {
	var rows []models.TeamRanking
	if err := r.db.Where("week_id = ? AND poll IN ?", weekID, []string{models.PollCFP, models.PollAP}).Find(&rows).Error; err != nil {
		return "", nil, err
	}

	cfp := make(map[uuid.UUID]int)
	ap := make(map[uuid.UUID]int)
	for _, row := range rows {
		switch row.Poll {
		case models.PollCFP:
			cfp[row.TeamID] = row.Rank
		case models.PollAP:
			ap[row.TeamID] = row.Rank
		}
	}

	if len(cfp) > 0 {
		return models.PollCFP, cfp, nil
	}
	if len(ap) > 0 {
		return models.PollAP, ap, nil
	}
	return "", nil, nil
}
