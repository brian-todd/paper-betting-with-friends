package cbbdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.collegebasketballdata.com"
	defaultTimeout = 30 * time.Second
)

// Client is an HTTP client for the College Basketball Data API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new CBB Data API client.
func NewClient(apiKey string) *Client {
	return NewClientAt(defaultBaseURL, apiKey)
}

// NewClientAt creates a client pointed at an arbitrary base URL, for replaying
// captured fixtures against a fake upstream.
//
// This is deliberately a constructor rather than a configuration value. An
// environment variable would put a production process one typo away from
// syncing a season out of a fixture directory and reporting success the whole
// time -- the exact failure class the fixture work exists to catch. A
// constructor is reachable only from code that means to call it, and the only
// callers are tests and `seed -fixtures`.
func NewClientAt(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// doRequest performs an authenticated GET request to the API.
func (c *Client) doRequest(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d: %s", resp.StatusCode, errorBody(resp.Body))
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}

// GetTeams retrieves all teams from the API.
func (c *Client) GetTeams(ctx context.Context) ([]APITeam, error) {
	var teams []APITeam
	if err := c.doRequest(ctx, "/teams", &teams); err != nil {
		return nil, fmt.Errorf("fetching teams: %w", err)
	}
	return teams, nil
}

// GetVenues retrieves all venues from the API.
func (c *Client) GetVenues(ctx context.Context) ([]APIVenue, error) {
	var venues []APIVenue
	if err := c.doRequest(ctx, "/venues", &venues); err != nil {
		return nil, fmt.Errorf("fetching venues: %w", err)
	}
	return venues, nil
}

// GetGames retrieves games from the API with optional filters.
func (c *Client) GetGames(ctx context.Context, opts GameQueryOpts) ([]APIGame, error) {
	var games []APIGame
	path := "/games?" + buildGameQuery(opts)
	if err := c.doRequest(ctx, path, &games); err != nil {
		return nil, fmt.Errorf("fetching games: %w", err)
	}
	return games, nil
}

// GetLines retrieves betting lines from the API with optional filters.
func (c *Client) GetLines(ctx context.Context, opts LineQueryOpts) ([]APIGameLines, error) {
	var lines []APIGameLines
	path := "/lines?" + buildLineQuery(opts)
	if err := c.doRequest(ctx, path, &lines); err != nil {
		return nil, fmt.Errorf("fetching lines: %w", err)
	}
	return lines, nil
}

// GetScoreboard retrieves the games the live feed currently covers. The endpoint
// requires Patreon Tier 1 or higher.
func (c *Client) GetScoreboard(ctx context.Context) ([]APIScoreboardGame, error) {
	var games []APIScoreboardGame
	if err := c.doRequest(ctx, "/scoreboard", &games); err != nil {
		return nil, fmt.Errorf("fetching scoreboard: %w", err)
	}
	return games, nil
}

// GetRankings retrieves every poll published so far in a season.
func (c *Client) GetRankings(ctx context.Context, season int) ([]APIPollRanking, error) {
	var rankings []APIPollRanking
	if err := c.doRequest(ctx, fmt.Sprintf("/rankings?season=%d", season), &rankings); err != nil {
		return nil, fmt.Errorf("fetching rankings: %w", err)
	}
	return rankings, nil
}

// GetGameLineups retrieves the five-man unit stats for one played game. An empty
// answer can mean not processed yet as well as never available.
func (c *Client) GetGameLineups(ctx context.Context, gameID int64) ([]APIGameLineup, error) {
	var lineups []APIGameLineup
	if err := c.doRequest(ctx, fmt.Sprintf("/lineups/game/%d", gameID), &lineups); err != nil {
		return nil, fmt.Errorf("fetching lineups for game %d: %w", gameID, err)
	}
	return lineups, nil
}

// GetAdjustedRatings retrieves every team's adjusted efficiency for a season. Empty
// until the provider has published any.
func (c *Client) GetAdjustedRatings(ctx context.Context, season int) ([]APIAdjustedRating, error) {
	var ratings []APIAdjustedRating
	if err := c.doRequest(ctx, fmt.Sprintf("/ratings/adjusted?season=%d", season), &ratings); err != nil {
		return nil, fmt.Errorf("fetching adjusted ratings: %w", err)
	}
	return ratings, nil
}

// GetSRSRatings retrieves every team's SRS rating for a season. Empty until the
// provider has published any.
func (c *Client) GetSRSRatings(ctx context.Context, season int) ([]APISRSRating, error) {
	var ratings []APISRSRating
	if err := c.doRequest(ctx, fmt.Sprintf("/ratings/srs?season=%d", season), &ratings); err != nil {
		return nil, fmt.Errorf("fetching srs ratings: %w", err)
	}
	return ratings, nil
}

// GetTeamSeasonStats retrieves every team's figures for a season.
func (c *Client) GetTeamSeasonStats(ctx context.Context, season int) ([]APITeamSeasonStats, error) {
	var stats []APITeamSeasonStats
	if err := c.doRequest(ctx, fmt.Sprintf("/stats/team/season?season=%d", season), &stats); err != nil {
		return nil, fmt.Errorf("fetching team season stats: %w", err)
	}
	return stats, nil
}

func buildGameQuery(opts GameQueryOpts) string {
	q := ""
	sep := ""
	if opts.Season != nil {
		q += fmt.Sprintf("%sseason=%d", sep, *opts.Season)
		sep = "&"
	}
	if opts.SeasonType != nil {
		q += fmt.Sprintf("%sseasonType=%s", sep, *opts.SeasonType)
		sep = "&"
	}
	if opts.StartDateRange != nil {
		q += fmt.Sprintf("%sstartDateRange=%s", sep, *opts.StartDateRange)
		sep = "&"
	}
	if opts.EndDateRange != nil {
		q += fmt.Sprintf("%sendDateRange=%s", sep, *opts.EndDateRange)
		sep = "&"
	}
	if opts.Team != nil {
		q += fmt.Sprintf("%steam=%s", sep, *opts.Team)
		sep = "&"
	}
	if opts.Conference != nil {
		q += fmt.Sprintf("%sconference=%s", sep, *opts.Conference)
		sep = "&"
	}
	if opts.Tournament != nil {
		q += fmt.Sprintf("%stournament=%s", sep, *opts.Tournament)
		sep = "&"
	}
	if opts.Status != nil {
		q += fmt.Sprintf("%sstatus=%s", sep, *opts.Status)
	}
	return q
}

func buildLineQuery(opts LineQueryOpts) string {
	q := ""
	sep := ""
	if opts.Season != nil {
		q += fmt.Sprintf("%sseason=%d", sep, *opts.Season)
		sep = "&"
	}
	if opts.StartDateRange != nil {
		q += fmt.Sprintf("%sstartDateRange=%s", sep, *opts.StartDateRange)
		sep = "&"
	}
	if opts.EndDateRange != nil {
		q += fmt.Sprintf("%sendDateRange=%s", sep, *opts.EndDateRange)
		sep = "&"
	}
	if opts.Team != nil {
		q += fmt.Sprintf("%steam=%s", sep, *opts.Team)
		sep = "&"
	}
	if opts.Conference != nil {
		q += fmt.Sprintf("%sconference=%s", sep, *opts.Conference)
	}
	return q
}

// errorBody reads a bounded snippet of a failed response for the error message.
//
// Without it the only thing a caller learns from a failure is a number. An
// upstream 502 usually says something about why, and the fixture server's own
// refusal is a sentence naming the directory to capture -- which was being
// thrown away at exactly the moment someone needed it.
func errorBody(body io.Reader) string {
	const limit = 512
	snippet, err := io.ReadAll(io.LimitReader(body, limit))
	if err != nil || len(snippet) == 0 {
		return "(no body)"
	}
	text := strings.TrimSpace(string(snippet))
	if len(snippet) == limit {
		text += "..."
	}
	return text
}
