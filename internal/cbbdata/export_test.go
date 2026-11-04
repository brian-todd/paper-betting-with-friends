package cbbdata

import "context"

// SyncLinesForTest exposes syncLines to the external test package.
//
// The level-2 tests have to live in package cbbdata_test, because fixtureseed
// imports this package. SyncLines, the scheduled job, takes its date window from
// the clock and names no season, so it asks for a query no capture answers. A
// replay of one captured month needs the window the seed used, which only
// syncLines takes.
func (s *SyncService) SyncLinesForTest(ctx context.Context, opts LineQueryOpts) error {
	return s.syncLines(ctx, opts)
}

// SyncReferenceForTest exposes the venue and team half of SeedAll, so a level-2
// test can write one captured day on top of it without seeding a whole season.
func (s *SyncService) SyncReferenceForTest(ctx context.Context) error {
	if err := s.syncVenues(ctx); err != nil {
		return err
	}
	return s.syncTeams(ctx)
}

// SyncGamesForTest exposes syncGames for the same reason SyncLinesForTest exists.
func (s *SyncService) SyncGamesForTest(ctx context.Context, opts GameQueryOpts) error {
	return s.syncGames(ctx, opts)
}
