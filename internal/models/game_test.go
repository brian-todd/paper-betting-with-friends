package models

import (
	"testing"

	"github.com/google/uuid"
)

// The feed labels a season by the year it ends in, so the label has to count back.
func TestBasketballSeasonLabel(t *testing.T) {
	for season, want := range map[int]string{2027: "2026-27", 2026: "2025-26", 2000: "1999-00"} {
		if got := BasketballSeasonLabel(season); got != want {
			t.Errorf("BasketballSeasonLabel(%d) = %q, want %q", season, got, want)
		}
	}
}

func TestParseSport(t *testing.T) {
	tests := []struct {
		value string
		want  string
		ok    bool
	}{
		{"", SportFootball, true},
		{"football", SportFootball, true},
		{"basketball", SportBasketball, true},
		{"curling", "", false},
		{"Basketball", "", false},
	}
	for _, tt := range tests {
		got, ok := ParseSport(tt.value)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseSport(%q) = %q, %v, want %q, %v", tt.value, got, ok, tt.want, tt.ok)
		}
	}
}

func TestGamePath(t *testing.T) {
	id := uuid.MustParse("0d6f7e1a-0000-4000-8000-000000000000")
	tests := []struct{ sport, want string }{
		{SportFootball, "/games/football/" + id.String()},
		{SportBasketball, "/games/basketball/" + id.String()},
		// Only a hand-built struct has no sport; the lookup route finds its page.
		{"", "/games/" + id.String()},
	}
	for _, tt := range tests {
		if got := (Game{ID: id, Sport: tt.sport}).Path(); got != tt.want {
			t.Errorf("Path() with sport %q = %q, want %q", tt.sport, got, tt.want)
		}
	}
}
