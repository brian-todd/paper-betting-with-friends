-- The basketball sync stored a result for any game the feed gave points to, and
-- the feed gives every scheduled, postponed and cancelled game 0-0. Those rows
-- put a score on games nobody has played. They carry no information: the sync
-- no longer writes them, and a real score arrives once the game is under way.
DELETE FROM game_results
USING games
WHERE game_results.game_id = games.id
  AND games.sport = 'basketball'
  AND games.status IN ('scheduled', 'postponed', 'cancelled')
  AND game_results.finalized_at IS NULL
  AND game_results.home_score = 0
  AND game_results.away_score = 0;
