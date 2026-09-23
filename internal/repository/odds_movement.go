package repository

import (
	"database/sql"

	"github.com/brian/paper-betting-with-friends/internal/models"
	"gorm.io/gorm"
)

// OddsMovementRepository records and reads how each book's line moved.
type OddsMovementRepository struct {
	db *gorm.DB
}

// NewOddsMovementRepository creates a new OddsMovementRepository.
func NewOddsMovementRepository(db *gorm.DB) *OddsMovementRepository {
	return &OddsMovementRepository{db: db}
}

// recordMovement inserts a movement unless its series already holds the same
// value as of that instant.
//
// The comparison is against the latest *movement*, not against the odds row the
// sync has just upserted. Two things follow from that. The odds upsert keeps
// bumping updated_at on every sync, which the game page shows as when the line
// was last checked. And a movement that fails to write is written by the next
// sync instead, one interval late, because the history still disagrees with the
// feed -- so the two writes need no transaction between them.
//
// "Latest" is the latest at or before the incoming instant, and the values are
// cast to the column types before they are compared, so a moneyline that arrives
// through decimal.NewFromFloat as 145 is the same line as a stored 145.00.
// IS NOT DISTINCT FROM makes an empty series -- no latest row -- a change, which
// is what records every series' first value without a backfill.
//
// The ON CONFLICT is a backstop for a series written twice at one instant. The
// syncs fold a book's quotes to one per run before writing -- see
// quotesBySource -- because a later quote replacing an earlier one here still
// leaves the next run's first quote reading as a move.
//
// Two things can leave a row that repeats the value before it, and neither is
// guarded against. A movement recorded earlier than one already stored is
// compared with what came before it, not after, so the later row may now repeat
// it -- only a replay records out of order. And two lines syncs running at once
// both read the same latest row, so both can record one move: a seed job
// overlapping the scheduled sync. Either way the line read at any instant is
// still right; the history holds a row that marks no change.
const recordMovement = `
WITH incoming AS (
	SELECT
		CAST(@game_id AS uuid)            AS game_id,
		CAST(@source AS varchar(50))      AS source,
		CAST(@market AS varchar(20))      AS market,
		CAST(@home_spread AS decimal(5,1)) AS home_spread,
		CAST(@total AS decimal(5,1))       AS total,
		CAST(@home_odds AS decimal(10,2))  AS home_odds,
		CAST(@away_odds AS decimal(10,2))  AS away_odds,
		CAST(@over_odds AS decimal(10,2))  AS over_odds,
		CAST(@under_odds AS decimal(10,2)) AS under_odds,
		CAST(@recorded_at AS timestamptz)  AS recorded_at
)
INSERT INTO odds_movements
	(game_id, source, market, home_spread, total, home_odds, away_odds, over_odds, under_odds, recorded_at)
SELECT
	i.game_id, i.source, i.market, i.home_spread, i.total, i.home_odds, i.away_odds, i.over_odds, i.under_odds, i.recorded_at
FROM incoming i
WHERE NOT EXISTS (
	SELECT 1
	FROM (
		SELECT m.home_spread, m.total, m.home_odds, m.away_odds, m.over_odds, m.under_odds
		FROM odds_movements m
		WHERE m.game_id = i.game_id AND m.market = i.market AND m.source = i.source
			AND m.recorded_at <= i.recorded_at
		ORDER BY m.recorded_at DESC
		LIMIT 1
	) latest
	WHERE (latest.home_spread, latest.total, latest.home_odds, latest.away_odds, latest.over_odds, latest.under_odds)
		IS NOT DISTINCT FROM
		(i.home_spread, i.total, i.home_odds, i.away_odds, i.over_odds, i.under_odds)
)
ON CONFLICT (game_id, market, source, recorded_at) DO UPDATE SET
	home_spread = EXCLUDED.home_spread,
	total = EXCLUDED.total,
	home_odds = EXCLUDED.home_odds,
	away_odds = EXCLUDED.away_odds,
	over_odds = EXCLUDED.over_odds,
	under_odds = EXCLUDED.under_odds`

// Record writes a movement if it changes its series, and reports whether it did.
//
// A sync calls this for every line it writes, changed or not; the decision is
// made here, in one statement, rather than by reading the series first.
func (r *OddsMovementRepository) Record(m models.OddsMovement) (bool, error) {
	result := r.db.Exec(recordMovement,
		sql.Named("game_id", m.GameID),
		sql.Named("source", string(m.Source)),
		sql.Named("market", string(m.Market)),
		sql.Named("home_spread", m.HomeSpread),
		sql.Named("total", m.Total),
		sql.Named("home_odds", m.HomeOdds),
		sql.Named("away_odds", m.AwayOdds),
		sql.Named("over_odds", m.OverOdds),
		sql.Named("under_odds", m.UnderOdds),
		sql.Named("recorded_at", m.RecordedAt),
	)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
