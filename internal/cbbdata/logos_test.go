package cbbdata

import (
	"testing"

	"github.com/brian/paper-betting-with-friends/internal/models"
)

func TestLogoFor(t *testing.T) {
	url := func(s string) *string { return &s }
	id := func(n int64) *int64 { return &n }

	book := newLogoBook([]models.Team{
		{ExternalID: id(150), Name: "Duke", LogoURL: url("duke-by-id")},
		{ExternalID: id(2608), Name: "St. Mary's (CA)", LogoURL: url("st-marys-by-id")},
		{ExternalID: id(2429), Name: "Charlotte", LogoURL: url("charlotte")},
		{ExternalID: id(3253), Name: "Charlotte"}, // a stub sharing the name
		{ExternalID: id(9000), Name: "Twin", LogoURL: url("first-twin")},
		{ExternalID: id(9001), Name: "Twin", LogoURL: url("second-twin")},
		{ExternalID: id(7), Name: "Same Name", LogoURL: url("the-other-school")},
	})

	tests := []struct {
		name string
		team APITeam
		want string
	}{
		{"by ID", APITeam{SourceID: "150", School: "Duke", Conference: "ACC"}, "duke-by-id"},
		{"by ID when the feeds spell the school differently", APITeam{SourceID: "2608", School: "Saint Mary's", Conference: "WCC"}, "st-marys-by-id"},
		{"ID before a name belonging to another school", APITeam{SourceID: "150", School: "Same Name", Conference: "ACC"}, "duke-by-id"},
		{"by name, past a stub with no logo", APITeam{SourceID: "3253", School: "Charlotte", Conference: "AAC"}, "charlotte"},
		{"by name, the same one every time", APITeam{SourceID: "1", School: "Twin"}, "first-twin"},
		{"ESPN for a conference team football cannot supply", APITeam{SourceID: "2670", School: "VCU", Conference: "A-10"}, "https://a.espncdn.com/i/teamlogos/ncaa/500/2670.png"},
		{"nothing for a team outside Division I", APITeam{SourceID: "510", School: "Culver-Stockton"}, ""},
		{"nothing for a placeholder ID", APITeam{SourceID: "UNKNOWN#Cotner College", School: "Cotner College", Conference: "ACC"}, ""},
		{"nothing for a missing ID", APITeam{School: "Nowhere", Conference: "ACC"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := book.logoFor(tt.team)
			switch {
			case tt.want == "" && got != nil:
				t.Errorf("logoFor() = %q, want none", *got)
			case tt.want != "" && (got == nil || *got != tt.want):
				t.Errorf("logoFor() = %v, want %q", got, tt.want)
			}
		})
	}
}
