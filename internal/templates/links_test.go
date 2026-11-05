package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var hardCodedGameURL = regexp.MustCompile(`"/games/((football|basketball)/)?\{\{|/basketball/games|"/basketball`)

// A game's URL depends on its sport, so a template links through Game.Path or
// Week.Path. A hard-coded one still works through a redirect, which is exactly why
// nothing else would notice it.
func TestTemplatesDoNotHardCodeGameURLs(t *testing.T) {
	found, err := filepath.Glob("../../templates/*/*.html")
	if err != nil {
		t.Fatalf("Glob error = %v", err)
	}
	if len(found) == 0 {
		t.Fatal("found no templates; the glob is probably wrong")
	}

	for _, file := range found {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		if match := hardCodedGameURL.Find(body); match != nil {
			t.Errorf("%s hard-codes a game URL: %s", filepath.Base(file), match)
		}
	}
}

func TestHardCodedGameURLPattern(t *testing.T) {
	tests := []struct {
		link string
		want bool
	}{
		{`href="/games/{{.Game.ID}}"`, true},
		{`href="/games/football/{{.Game.ID}}"`, true},
		{`href="/games/basketball/{{.Game.ID}}"`, true},
		{`href="/basketball"`, true},
		{`href="/basketball/games?date={{.Day}}"`, true},
		{`href="{{.Game.Path}}"`, false},
		// A day and the admin's pages are not a game's page.
		{`href="/games/basketball?date={{.BasketballDay}}"`, false},
		{`href="/admin/games/{{.ID}}"`, false},
	}
	for _, tt := range tests {
		if got := hardCodedGameURL.MatchString(tt.link); got != tt.want {
			t.Errorf("matching %s = %v, want %v", tt.link, got, tt.want)
		}
	}
}
