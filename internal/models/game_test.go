package models

import "testing"

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
