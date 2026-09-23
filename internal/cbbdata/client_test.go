package cbbdata

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
			{"id": 1, "school": "Duke", "mascot": "Blue Devils", "currentVenueId": 42},
			{"id": 2, "school": "Kansas", "mascot": "Jayhawks", "currentVenueId": null}
		]`))
	})

	teams, err := c.GetTeams(context.Background())
	if err != nil {
		t.Fatalf("GetTeams() error = %v", err)
	}
	if len(teams) != 2 {
		t.Fatalf("got %d teams, want 2", len(teams))
	}
	if teams[0].CurrentVenueID == nil || *teams[0].CurrentVenueID != 42 {
		t.Errorf("teams[0].CurrentVenueID = %v, want 42", teams[0].CurrentVenueID)
	}
	// A team without a venue must decode to nil rather than zero, since the
	// sync uses nil to decide whether to create a placeholder venue.
	if teams[1].CurrentVenueID != nil {
		t.Errorf("teams[1].CurrentVenueID = %v, want nil", *teams[1].CurrentVenueID)
	}
}

func TestGetVenuesDecodesResponse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id": 42, "name": "Cameron Indoor Stadium", "city": "Durham", "state": "NC"}]`))
	})

	venues, err := c.GetVenues(context.Background())
	if err != nil {
		t.Fatalf("GetVenues() error = %v", err)
	}
	if len(venues) != 1 || venues[0].Name != "Cameron Indoor Stadium" {
		t.Fatalf("venues = %+v, want one Cameron Indoor Stadium", venues)
	}
}

func TestGetGamesBuildsQuery(t *testing.T) {
	season := 2025
	seasonType := "regular"
	start := "2025-01-01"
	end := "2025-01-31"

	tests := []struct {
		name      string
		opts      GameQueryOpts
		wantQuery string
	}{
		{"empty", GameQueryOpts{}, ""},
		{"season only", GameQueryOpts{Season: &season}, "season=2025"},
		{
			"season and type",
			GameQueryOpts{Season: &season, SeasonType: &seasonType},
			"season=2025&seasonType=regular",
		},
		{
			"date range",
			GameQueryOpts{Season: &season, StartDateRange: &start, EndDateRange: &end},
			"season=2025&startDateRange=2025-01-01&endDateRange=2025-01-31",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotQuery, gotPath string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotQuery = r.URL.RawQuery
				w.Write([]byte(`[]`))
			})

			if _, err := c.GetGames(context.Background(), tt.opts); err != nil {
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

func TestGetLinesBuildsQuery(t *testing.T) {
	season := 2025
	team := "Duke"

	var gotQuery, gotPath string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`[]`))
	})

	if _, err := c.GetLines(context.Background(), LineQueryOpts{Season: &season, Team: &team}); err != nil {
		t.Fatalf("GetLines() error = %v", err)
	}
	if gotPath != "/lines" {
		t.Errorf("path = %q, want /lines", gotPath)
	}
	if want := "season=2025&team=Duke"; gotQuery != want {
		t.Errorf("query = %q, want %q", gotQuery, want)
	}
}

func TestClientErrorsOnNonOKStatus(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})

	_, err := c.GetTeams(context.Background())
	if err == nil {
		t.Fatal("GetTeams() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %q, want it to mention status 401", err)
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
