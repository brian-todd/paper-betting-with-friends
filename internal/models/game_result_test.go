package models

import (
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
