package cbbdata

import (
	"fmt"
	"strconv"

	"github.com/brian/paper-betting-with-friends/internal/models"
)

// espnLogoURL is a team's logo on ESPN's image host, by the ID the feed's sourceId
// carries. It is not a documented API, so nothing depends on it beyond the picture:
// removing it from logoBook.logoFor and re-running the seed takes it out.
const espnLogoURL = "https://a.espncdn.com/i/teamlogos/ncaa/500/%d.png"

// logoBook holds the logos football's feed supplies, since basketball's has none.
type logoBook struct {
	byExternalID map[int64]string
	byName       map[string]string
}

// newLogoBook indexes football teams. Two football rows can share a name, usually a
// real team and a stub without a logo, so these are the rows with a logo, and the
// first of them wins a name so that a re-seed does not change its mind.
func newLogoBook(football []models.Team) logoBook {
	book := logoBook{
		byExternalID: make(map[int64]string, len(football)),
		byName:       make(map[string]string, len(football)),
	}
	for _, team := range football {
		if team.LogoURL == nil {
			continue
		}
		if team.ExternalID != nil {
			book.byExternalID[*team.ExternalID] = *team.LogoURL
		}
		if _, taken := book.byName[team.Name]; !taken {
			book.byName[team.Name] = *team.LogoURL
		}
	}
	return book
}

// logoFor finds a basketball team's logo, best evidence first.
//
// sourceId is the same number as football's team ID, so it is tried before the name:
// the two feeds spell schools differently. ESPN is the last resort and only for a team
// with a conference, which in this feed means Division I. It has every one of those
// and few of the 1,150 others, which appear as opponents and would show a broken image.
func (b logoBook) logoFor(t APITeam) *string {
	id, err := strconv.ParseInt(t.SourceID, 10, 64)
	hasID := err == nil && id > 0

	if hasID {
		if logo, ok := b.byExternalID[id]; ok {
			return &logo
		}
	}
	if logo, ok := b.byName[t.School]; ok {
		return &logo
	}
	if hasID && t.Conference != "" {
		logo := fmt.Sprintf(espnLogoURL, id)
		return &logo
	}
	return nil
}
