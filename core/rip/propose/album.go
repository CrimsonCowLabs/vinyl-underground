package propose

import (
	"cmp"
	"strings"

	"github.com/navidrome/navidrome/core/musicbrainz"
)

// Choice is the release group Pick chose for a Track's album.
type Choice struct {
	Album       string
	AlbumArtist string
	// Year is the earliest year of the release group's releases the Track is on, "" if unknown.
	Year           string
	Type           string
	ReleaseGroupID string
	// ByArtist is true when the release is credited to the Track's main artist.
	ByArtist bool
}

// typeRank orders the release group types Pick accepts: a studio album first, then an EP, then
// the single.
var typeRank = map[string]int{"album": 0, "ep": 1, "single": 2}

// Pick chooses the album for a Track from the releases its recordings are on: the earliest
// official studio album by the main artist, else an EP, else the single. Only official releases
// count, and never one whose release group has a secondary type (a compilation, live, remix,
// soundtrack or DJ-mix release), or is credited to "Various Artists". A release by the main
// artist beats any other, whatever its type. It returns nil when no release qualifies.
func Pick(releases []musicbrainz.Release, mainArtist string) *Choice {
	type candidate struct {
		choice Choice
		mine   int
		rank   int
		date   string
	}
	best := map[string]candidate{}
	var order []string
	for _, r := range releases {
		rg := r.ReleaseGroup
		rank, ok := typeRank[strings.ToLower(rg.PrimaryType)]
		if !ok || !strings.EqualFold(r.Status, "official") || len(rg.SecondaryTypes) > 0 || r.ArtistCredit.IsVariousArtists() {
			continue
		}
		credit := r.ArtistCredit.String()
		mine := boolRank(credit == "" || sameArtist(credit, mainArtist))
		date := earliest(r.Date, rg.FirstReleaseDate)
		c := candidate{
			choice: Choice{
				Album: cmp.Or(rg.Title, r.Title), AlbumArtist: credit, Year: year(date), Type: rg.PrimaryType,
				ReleaseGroupID: rg.ID, ByArtist: mine == 0,
			},
			mine: mine, rank: rank, date: date,
		}
		key := cmp.Or(rg.ID, r.ID)
		prev, seen := best[key]
		if !seen {
			order = append(order, key)
		}
		if !seen || c.date < prev.date {
			best[key] = c
		}
	}
	var res *candidate
	for _, key := range order {
		c := best[key]
		if res == nil || cmp.Or(
			cmp.Compare(c.mine, res.mine),
			cmp.Compare(c.rank, res.rank),
			cmp.Compare(c.date[:4], res.date[:4]),
			cmp.Compare(c.date, res.date),
		) < 0 {
			res = &c
		}
	}
	if res == nil {
		return nil
	}
	return &res.choice
}

// earliest is the earlier of two MusicBrainz dates ("YYYY", "YYYY-MM" or "YYYY-MM-DD"), or "9999"
// when neither is known, so a release with no date sorts last.
func earliest(a, b string) string {
	res := "9999"
	for _, d := range []string{a, b} {
		if len(d) >= 4 && d < res {
			res = d
		}
	}
	return res
}

func year(date string) string {
	if date == "9999" {
		return ""
	}
	return date[:4]
}
