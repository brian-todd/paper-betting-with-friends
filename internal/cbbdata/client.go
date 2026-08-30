package cbbdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	return &Client{
		baseURL: defaultBaseURL,
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
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
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
