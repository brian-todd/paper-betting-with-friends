package cfbdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient points a Client at a fake upstream. httptest.NewTestServer
// registers its own cleanup against t, so the server needs no explicit Close.
// Start gives it a loopback listener and a real URL, which is what Client
// needs since it dials with its own http.Client.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewTestServer(t, handler)
	srv.Start()
	c := NewClient("test-api-key")
	c.baseURL = srv.URL
	return c
}

func TestClientSendsAuthHeaders(t *testing.T) {
	var gotAuth, gotAccept string

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		w.Write([]byte(`[]`))
	})

	if _, err := c.GetTeams(context.Background()); err != nil {
		t.Fatalf("GetTeams() error = %v", err)
	}

	if want := "Bearer test-api-key"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if want := "application/json"; gotAccept != want {
		t.Errorf("Accept = %q, want %q", gotAccept, want)
	}
}

func TestGetTeamsDecodesResponse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/teams" {
			t.Errorf("path = %q, want /teams", r.URL.Path)
		}
		w.Write([]byte(`[
			{"id": 333, "school": "Alabama", "mascot": "Crimson Tide", "conference": "SEC"},
			{"id": 99,  "school": "Auburn",  "mascot": "Tigers",       "conference": "SEC"}
		]`))
	})

	teams, err := c.GetTeams(context.Background())
	if err != nil {
		t.Fatalf("GetTeams() error = %v", err)
	}
	if len(teams) != 2 {
		t.Fatalf("got %d teams, want 2", len(teams))
	}
	if teams[0].School != "Alabama" || teams[0].ID != 333 {
		t.Errorf("teams[0] = %+v, want Alabama/333", teams[0])
	}
	if teams[1].Conference != "SEC" {
		t.Errorf("teams[1].Conference = %q, want SEC", teams[1].Conference)
	}
}

func TestGetVenuesDecodesResponse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id": 1, "name": "Bryant-Denny Stadium", "city": "Tuscaloosa", "state": "AL", "capacity": 100077}]`))
	})

	venues, err := c.GetVenues(context.Background())
	if err != nil {
		t.Fatalf("GetVenues() error = %v", err)
	}
	if len(venues) != 1 {
		t.Fatalf("got %d venues, want 1", len(venues))
	}
	if venues[0].Capacity != 100077 {
		t.Errorf("Capacity = %d, want 100077", venues[0].Capacity)
	}
}

// The optional week/seasonType filters must only appear when set.
func TestGetGamesBuildsQuery(t *testing.T) {
	week := 5
	seasonType := "regular"

	tests := []struct {
		name       string
		week       *int
		seasonType *string
		wantQuery  string
	}{
		{"year only", nil, nil, "year=2024"},
		{"with week", &week, nil, "year=2024&week=5"},
		{"with season type", nil, &seasonType, "year=2024&seasonType=regular"},
		{"all filters", &week, &seasonType, "year=2024&week=5&seasonType=regular"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotQuery, gotPath string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotQuery = r.URL.RawQuery
				w.Write([]byte(`[]`))
			})

			if _, err := c.GetGames(context.Background(), 2024, tt.week, tt.seasonType); err != nil {
				t.Fatalf("GetGames() error = %v", err)
			}
			if gotPath != "/games" {
				t.Errorf("path = %q, want /games", gotPath)
			}
			if gotQuery != tt.wantQuery {
				t.Errorf("query = %q, want %q", gotQuery, tt.wantQuery)
			}
		})
	}
}

func TestGetCalendarBuildsQuery(t *testing.T) {
	var gotQuery string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`[{"season": 2024, "week": 1, "seasonType": "regular",
			"startDate": "2024-08-24T00:00:00.000Z", "endDate": "2024-08-31T00:00:00.000Z"}]`))
	})

	weeks, err := c.GetCalendar(context.Background(), 2024)
	if err != nil {
		t.Fatalf("GetCalendar() error = %v", err)
	}
	if gotQuery != "year=2024" {
		t.Errorf("query = %q, want year=2024", gotQuery)
	}
	if len(weeks) != 1 || weeks[0].Week != 1 {
		t.Fatalf("weeks = %+v, want one week 1", weeks)
	}
	if got := weeks[0].StartDate.Format("2006-01-02"); got != "2024-08-24" {
		t.Errorf("StartDate = %s, want 2024-08-24", got)
	}
}

func TestGetRankingsBuildsQueryAndDecodesNestedPolls(t *testing.T) {
	week := 10
	seasonType := "regular"

	var gotPath, gotQuery string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`[
			{
				"season": 2025,
				"seasonType": "regular",
				"week": 10,
				"polls": [
					{
						"poll": "Playoff Committee Rankings",
						"ranks": [
							{"rank": 1, "school": "Ohio State", "firstPlaceVotes": null, "points": null},
							{"rank": 2, "school": "Indiana", "firstPlaceVotes": null, "points": null}
						]
					},
					{
						"poll": "AP Top 25",
						"ranks": [
							{"rank": 1, "school": "Ohio State", "firstPlaceVotes": 60, "points": 1500}
						]
					}
				]
			}
		]`))
	})

	weeks, err := c.GetRankings(context.Background(), 2025, &week, &seasonType)
	if err != nil {
		t.Fatalf("GetRankings() error = %v", err)
	}

	if gotPath != "/rankings" {
		t.Errorf("path = %q, want /rankings", gotPath)
	}
	if want := "year=2025&week=10&seasonType=regular"; gotQuery != want {
		t.Errorf("query = %q, want %q", gotQuery, want)
	}

	if len(weeks) != 1 {
		t.Fatalf("got %d ranking weeks, want 1", len(weeks))
	}
	rw := weeks[0]
	if rw.Season != 2025 || rw.Week != 10 || rw.SeasonType != "regular" {
		t.Errorf("ranking week = %+v, want season 2025 week 10 regular", rw)
	}
	if len(rw.Polls) != 2 {
		t.Fatalf("got %d polls, want 2", len(rw.Polls))
	}
	if rw.Polls[0].Poll != "Playoff Committee Rankings" || len(rw.Polls[0].Ranks) != 2 {
		t.Errorf("polls[0] = %+v, want CFP poll with 2 ranks", rw.Polls[0])
	}
	if got := rw.Polls[0].Ranks[1].School; got != "Indiana" {
		t.Errorf("polls[0].Ranks[1].School = %q, want Indiana", got)
	}
	ap := rw.Polls[1]
	if ap.Poll != "AP Top 25" || len(ap.Ranks) != 1 {
		t.Fatalf("polls[1] = %+v, want AP poll with 1 rank", ap)
	}
	if ap.Ranks[0].FirstPlaceVotes == nil || *ap.Ranks[0].FirstPlaceVotes != 60 {
		t.Errorf("FirstPlaceVotes = %v, want 60", ap.Ranks[0].FirstPlaceVotes)
	}
	if ap.Ranks[0].Points == nil || *ap.Ranks[0].Points != 1500 {
		t.Errorf("Points = %v, want 1500", ap.Ranks[0].Points)
	}
}

func TestGetRankingsOmitsUnsetFilters(t *testing.T) {
	var gotQuery string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`[]`))
	})

	if _, err := c.GetRankings(context.Background(), 2025, nil, nil); err != nil {
		t.Fatalf("GetRankings() error = %v", err)
	}
	if gotQuery != "year=2025" {
		t.Errorf("query = %q, want year=2025", gotQuery)
	}
}

func TestClientErrorsOnNonOKStatus(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	})

	_, err := c.GetTeams(context.Background())
	if err == nil {
		t.Fatal("GetTeams() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q, want it to mention status 429", err)
	}
}

func TestClientErrorsOnMalformedJSON(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{not json`))
	})

	_, err := c.GetTeams(context.Background())
	if err == nil {
		t.Fatal("GetTeams() error = nil, want a decode error")
	}
	if !strings.Contains(err.Error(), "decoding response") {
		t.Errorf("error = %q, want it to mention decoding", err)
	}
}

func TestClientHonorsContextCancellation(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.GetTeams(ctx); err == nil {
		t.Fatal("GetTeams() error = nil, want a context error")
	}
}
