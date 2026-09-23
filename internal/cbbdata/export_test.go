package cbbdata

import "context"

// SyncLinesForTest exposes syncLines to the external test package.
//
// The level-2 tests have to live in package cbbdata_test, because fixtureseed
// imports this package. Football's SyncLines is exported for its scheduled job;
// basketball's lines run inside SyncGamesAndLines, whose date window comes from
// the clock and names no season, so it asks for a query no capture answers. A
// replay of one captured month needs the window the seed used, which only
// syncLines takes.
func (s *SyncService) SyncLinesForTest(ctx context.Context, opts LineQueryOpts) error {
	return s.syncLines(ctx, opts)
}
