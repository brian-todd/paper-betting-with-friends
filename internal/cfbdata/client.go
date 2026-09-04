package cfbdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	defaultBaseURL = "https://api.collegefootballdata.com"
	defaultTimeout = 30 * time.Second
)

// Client is an HTTP client for the College Football Data API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new CFB Data API client.
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

// GetCalendar retrieves the calendar (weeks) for a given year.
func (c *Client) GetCalendar(ctx context.Context, year int) ([]APIWeek, error) {
	var weeks []APIWeek
	path := fmt.Sprintf("/calendar?year=%d", year)
	if err := c.doRequest(ctx, path, &weeks); err != nil {
		return nil, fmt.Errorf("fetching calendar: %w", err)
	}
	return weeks, nil
}

// GetGames retrieves games for a given year, optionally filtered by week and season type.
func (c *Client) GetGames(ctx context.Context, year int, week *int, seasonType *string) ([]APIGame, error) {
	var games []APIGame
	path := fmt.Sprintf("/games?year=%d", year)
	if week != nil {
		path += fmt.Sprintf("&week=%d", *week)
	}
	if seasonType != nil {
		path += fmt.Sprintf("&seasonType=%s", *seasonType)
	}
	if err := c.doRequest(ctx, path, &games); err != nil {
		return nil, fmt.Errorf("fetching games: %w", err)
	}
	return games, nil
}

// GetRankings retrieves poll rankings for a given year, optionally filtered by
// week and season type. Called with week nil, it returns the whole season's
// rankings history in one request.
func (c *Client) GetRankings(ctx context.Context, year int, week *int, seasonType *string) ([]APIRankingWeek, error) {
	var weeks []APIRankingWeek
	path := fmt.Sprintf("/rankings?year=%d", year)
	if week != nil {
		path += fmt.Sprintf("&week=%d", *week)
	}
	if seasonType != nil {
		path += fmt.Sprintf("&seasonType=%s", *seasonType)
	}
	if err := c.doRequest(ctx, path, &weeks); err != nil {
		return nil, fmt.Errorf("fetching rankings: %w", err)
	}
	return weeks, nil
}

// GetLines retrieves betting lines for a given year, optionally filtered by week and season type.
func (c *Client) GetLines(ctx context.Context, year int, week *int, seasonType *string) ([]APILine, error) {
	var lines []APILine
	path := fmt.Sprintf("/lines?year=%d", year)
	if week != nil {
		path += fmt.Sprintf("&week=%d", *week)
	}
	if seasonType != nil {
		path += fmt.Sprintf("&seasonType=%s", *seasonType)
	}
	if err := c.doRequest(ctx, path, &lines); err != nil {
		return nil, fmt.Errorf("fetching lines: %w", err)
	}
	return lines, nil
}
