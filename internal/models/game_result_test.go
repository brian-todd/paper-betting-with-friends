package models

import (
	"strconv"
	"testing"
	"time"
)

func TestIntSliceValue(t *testing.T) {
	tests := []struct {
		name     string
		input    IntSlice
		expected string
		isNil    bool
	}{
		{"nil slice", nil, "", true},
		{"empty slice", IntSlice{}, "[]", false},
		{"quarter scores", IntSlice{7, 14, 7, 3}, "[7,14,7,3]", false},
		{"single element", IntSlice{21}, "[21]", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.input.Value()
			if err != nil {
				t.Fatalf("Value() error = %v", err)
			}
			if tt.isNil {
				if got != nil {
					t.Errorf("Value() = %v, want nil", got)
				}
				return
			}
			bytes, ok := got.([]byte)
			if !ok {
				t.Fatalf("Value() returned %T, want []byte", got)
			}
			if string(bytes) != tt.expected {
				t.Errorf("Value() = %s, want %s", bytes, tt.expected)
			}
		})
	}
}

func TestIntSliceScan(t *testing.T) {
	t.Run("nil value", func(t *testing.T) {
		var s IntSlice
		if err := s.Scan(nil); err != nil {
			t.Fatalf("Scan(nil) error = %v", err)
		}
		if s != nil {
			t.Errorf("Scan(nil) = %v, want nil", s)
		}
	})

	t.Run("valid JSON", func(t *testing.T) {
		var s IntSlice
		if err := s.Scan([]byte(`[7,14,7,3]`)); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		expected := IntSlice{7, 14, 7, 3}
		if len(s) != len(expected) {
			t.Fatalf("Scan() len = %d, want %d", len(s), len(expected))
		}
		for i, v := range s {
			if v != expected[i] {
				t.Errorf("Scan()[%d] = %d, want %d", i, v, expected[i])
			}
		}
	})

	t.Run("empty array", func(t *testing.T) {
		var s IntSlice
		if err := s.Scan([]byte(`[]`)); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		if len(s) != 0 {
			t.Errorf("Scan([]) len = %d, want 0", len(s))
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		var s IntSlice
		err := s.Scan("not bytes")
		if err == nil {
			t.Error("Scan(string) expected error, got nil")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		var s IntSlice
		err := s.Scan([]byte(`not json`))
		if err == nil {
			t.Error("Scan(invalid JSON) expected error, got nil")
		}
	})
}

func TestGameResultIsFinal(t *testing.T) {
	finalizedAt := time.Now()

	tests := []struct {
		name   string
		result *GameResult
		want   bool
	}{
		{"finalized result", &GameResult{FinalizedAt: &finalizedAt}, true},
		// A live game has a score but no finalized time. Bet settlement reads
		// this, so getting it wrong pays out on a game still being played.
		{"live score", &GameResult{HomeScore: 21, AwayScore: 14}, false},
		// Callers hold a *GameResult that is nil when no score exists yet, so
		// the nil receiver has to answer rather than panic.
		{"no result at all", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsFinal(); got != tt.want {
				t.Errorf("IsFinal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGameResultPeriodScores(t *testing.T) {
	tests := []struct {
		name       string
		result     *GameResult
		wantLabels []string
		wantHome   []string
		wantAway   []string
	}{
		{
			// Every game before kickoff, and every feed that reports no
			// breakdown. The caller draws no table rather than an empty one.
			name:   "nil receiver",
			result: nil,
		},
		{
			name:   "no line scores",
			result: &GameResult{HomeScore: 10, AwayScore: 7},
		},
		{
			name: "regulation",
			result: &GameResult{
				HomeLineScores: IntSlice{14, 21, 24, 0},
				AwayLineScores: IntSlice{0, 7, 0, 3},
			},
			wantLabels: []string{"Q1", "Q2", "Q3", "Q4"},
			wantHome:   []string{"14", "21", "24", "0"},
			wantAway:   []string{"0", "7", "0", "3"},
		},
		{
			// The labels come from the same helper the live badge uses, so an
			// overtime column and an overtime badge cannot disagree.
			name: "double overtime",
			result: &GameResult{
				HomeLineScores: IntSlice{7, 7, 7, 7, 7, 3},
				AwayLineScores: IntSlice{7, 7, 7, 7, 7, 0},
			},
			wantLabels: []string{"Q1", "Q2", "Q3", "Q4", "OT", "2OT"},
			wantHome:   []string{"7", "7", "7", "7", "7", "3"},
			wantAway:   []string{"7", "7", "7", "7", "7", "0"},
		},
		{
			// The two arrays arrive independently from an upstream nobody here
			// controls. Indexing one by the other's length is how that becomes
			// a panic on a page, so the short side reports no entry instead.
			name: "sides of unequal length",
			result: &GameResult{
				HomeLineScores: IntSlice{7, 3, 10},
				AwayLineScores: IntSlice{0},
			},
			wantLabels: []string{"Q1", "Q2", "Q3"},
			wantHome:   []string{"7", "3", "10"},
			wantAway:   []string{"0", "-", "-"},
		},
		{
			name:       "only one side reported",
			result:     &GameResult{AwayLineScores: IntSlice{0, 7}},
			wantLabels: []string{"Q1", "Q2"},
			wantHome:   []string{"-", "-"},
			wantAway:   []string{"0", "7"},
		},
	}

	show := func(v *int) string {
		if v == nil {
			return "-"
		}
		return strconv.Itoa(*v)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scores := tt.result.PeriodScores()
			if len(scores) != len(tt.wantLabels) {
				t.Fatalf("got %d periods, want %d", len(scores), len(tt.wantLabels))
			}

			for i, score := range scores {
				if score.Label != tt.wantLabels[i] {
					t.Errorf("period %d label = %q, want %q", i, score.Label, tt.wantLabels[i])
				}
				if got := show(score.Home); got != tt.wantHome[i] {
					t.Errorf("period %d home = %s, want %s", i, got, tt.wantHome[i])
				}
				if got := show(score.Away); got != tt.wantAway[i] {
					t.Errorf("period %d away = %s, want %s", i, got, tt.wantAway[i])
				}
			}
		})
	}
}
