package metadata

import (
	"slices"
	"strings"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/utils/str"
)

// Corrections are the admin's fixes to tag values (see model.Correction), ready to apply to Files
// as they are read. The zero value applies nothing.
type Corrections struct {
	renames  map[string]model.Correction // artist renames being applied, by MatchKey
	undoings []model.Correction          // artist renames being undone
}

// NewCorrections prepares the Corrections for reading Files.
func NewCorrections(cs model.Corrections) Corrections {
	c := Corrections{renames: make(map[string]model.Correction)}
	for _, cr := range cs {
		if cr.Kind != model.CorrectionArtistRename {
			continue
		}
		if cr.Undoing {
			c.undoings = append(c.undoings, cr)
			continue
		}
		c.renames[cr.MatchKey] = cr
	}
	return c
}

// IDMove says that what was attached to one ID (e.g. stars and play counts) belongs to another.
type IDMove struct {
	From, To string
}

// ArtistIDMoves returns where each renamed Artist's ID moves: from the old name's ID to the new
// one's, or back for a rename being undone. A rename that only changes capitals or spaces keeps
// the ID, so it moves nothing.
func (c Corrections) ArtistIDMoves() []IDMove {
	var moves []IDMove
	add := func(from, to string) {
		if m := (IDMove{From: ArtistID(from), To: ArtistID(to)}); m.From != m.To {
			moves = append(moves, m)
		}
	}
	for _, cr := range c.renames {
		add(cr.From, c.resolve(cr.To, nil))
	}
	for _, cr := range c.undoings {
		add(c.resolve(cr.To, nil), cr.From)
	}
	return moves
}

// ArtistID returns the ID of the Artist with the given name.
func ArtistID(name string) string {
	return Metadata{}.artistID(name)
}

// resolve returns the name an Artist is renamed to, following renames of renamed Artists (and
// stopping at a loop), and appends the IDs of the Corrections it applied to applied.
func (c Corrections) resolve(name string, applied *[]string) string {
	var seen []string
	for {
		cr, ok := c.renames[model.ArtistNameKey(name)]
		if !ok || slices.Contains(seen, cr.ID) {
			break
		}
		seen = append(seen, cr.ID)
		name = cr.To
	}
	if applied != nil {
		*applied = append(*applied, seen...)
	}
	return name
}

// renameArtists applies the artist renames to the Track artists and album artists, and to their
// display names. Other credits, like composers, keep the tagged names.
func (c Corrections) renameArtists(mf *model.MediaFile) {
	if len(c.renames) == 0 {
		return
	}
	var applied []string
	renameRole := func(role model.Role, display string) string {
		list := mf.Participants[role]
		renamed := make(model.ParticipantList, 0, len(list))
		for _, p := range list {
			if to := c.resolve(p.Name, &applied); to != p.Name {
				display = replaceName(display, p.Name, to)
				p.Name = to
				p.ID = ArtistID(to)
				p.OrderArtistName = str.SanitizeFieldForSortingNoArticle(to)
				p.SortArtistName = ""
			}
			// Two names can be renamed to the same Artist, who is credited once
			if !slices.ContainsFunc(renamed, func(q model.Participant) bool { return q.ID == p.ID && q.SubRole == p.SubRole }) {
				renamed = append(renamed, p)
			}
		}
		if len(list) > 0 {
			mf.Participants[role] = renamed
		}
		return display
	}
	mf.Artist = renameRole(model.RoleArtist, mf.Artist)
	mf.AlbumArtist = renameRole(model.RoleAlbumArtist, mf.AlbumArtist)
	slices.Sort(applied)
	mf.CorrectionIDs = slices.Compact(applied)
}

// replaceName swaps the name for its new one in a display name, which joins the tagged names.
// A display name of just that Artist (give or take spaces) becomes the new name.
func replaceName(display, name, to string) string {
	if model.ArtistNameKey(display) == model.ArtistNameKey(name) {
		return to
	}
	return strings.Replace(display, name, to, 1)
}
