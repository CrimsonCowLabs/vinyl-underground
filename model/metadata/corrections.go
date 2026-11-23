package metadata

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/utils/str"
)

// Corrections are the admin's fixes to tag values (see model.Correction), ready to apply to Files
// as they are read. The zero value applies nothing.
type Corrections struct {
	renames  map[string]model.Correction // artist renames (and merges) being applied, by MatchKey
	undoings []model.Correction          // artist renames (and merges) being undone
	albums   map[string]model.Correction // album Corrections being applied, by the folders they match
	numbers  map[fileKey]trackNumbers    // track numbers being applied, by File
}

type fileKey struct {
	libID int
	path  string
}

// trackNumbers is a track numbers Correction's fix for one File.
type trackNumbers struct {
	correctionID string
	fix          model.TrackNumberFix
}

// NewCorrections prepares the Corrections for reading Files. An album Correction matches only the
// folder it is keyed by until MatchAlbumFolders adds the album's other folders.
func NewCorrections(cs model.Corrections) Corrections {
	c := Corrections{renames: make(map[string]model.Correction), albums: make(map[string]model.Correction),
		numbers: make(map[fileKey]trackNumbers)}
	for _, cr := range cs {
		if cr.Kind == model.CorrectionAlbum && !cr.Undoing {
			c.albums[cr.MatchKey] = cr
		}
		if cr.Kind == model.CorrectionTrackNumbers && !cr.Undoing {
			// Should two Corrections renumber one File, the first listed (the newest) wins
			for _, f := range cr.TrackNumbers {
				k := fileKey{f.LibraryID, f.Path}
				if _, taken := c.numbers[k]; !taken {
					c.numbers[k] = trackNumbers{correctionID: cr.ID, fix: f}
				}
			}
		}
		if !cr.Kind.IsArtistRename() {
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

// MatchAlbumFolders makes the album Correction keyed by key also match Files in the folders (see
// model.CorrectionRepository.AlbumFolderIDs). A folder keeps the Correction keyed by it.
func (c Corrections) MatchAlbumFolders(key string, folderIDs []string) {
	cr, ok := c.albums[key]
	if !ok {
		return
	}
	for _, f := range folderIDs {
		if _, taken := c.albums[f]; !taken {
			c.albums[f] = cr
		}
	}
}

// correctAlbum returns the File's tags with the album Correction matching its folder applied, as
// if the File were tagged with the corrected values, and the Correction's ID. ok is false when no
// album Correction matches.
func (c Corrections) correctAlbum(md Metadata, folderID string) (corrected Metadata, id string, ok bool) {
	cr, ok := c.albums[folderID]
	if !ok {
		return md, "", false
	}
	tags := maps.Clone(md.tags)
	if cr.AlbumName != "" {
		tags[model.TagAlbum] = []string{cr.AlbumName}
		delete(tags, model.TagAlbumSort)
	}
	if cr.AlbumArtist != "" {
		tags[model.TagAlbumArtist] = []string{cr.AlbumArtist}
		for _, t := range []model.TagName{model.TagAlbumArtists, model.TagAlbumArtistSort,
			model.TagAlbumArtistsSort, model.TagMusicBrainzAlbumArtistID} {
			delete(tags, t)
		}
	}
	if cr.Year > 0 {
		correctYear(tags, cr.Year)
	}
	// The release and its release group, which key the album (the default album PID) and lead
	// to its Cover Art Archive cover and its info. The Track's own MusicBrainz IDs and the album
	// artist's stay as tagged.
	if cr.MbzAlbumID != "" {
		tags[model.TagMusicBrainzAlbumID] = []string{cr.MbzAlbumID}
	}
	if cr.MbzReleaseGroupID != "" {
		tags[model.TagMusicBrainzReleaseGroupID] = []string{cr.MbzReleaseGroupID}
	}
	md.tags = tags
	return md, cr.ID, true
}

// correctTrackNumbers returns the File's tags with the track numbers Correction for the File
// applied, as if the File were tagged with the corrected numbers, and the Correction's ID. A
// number the Correction leaves as tagged keeps its tag. ok is false when none applies.
func (c Corrections) correctTrackNumbers(md Metadata, libID int) (corrected Metadata, id string, ok bool) {
	n, ok := c.numbers[fileKey{libID, md.FilePath()}]
	if !ok {
		return md, "", false
	}
	tags := maps.Clone(md.tags)
	setNumber(tags, model.TagTrackNumber, n.fix.FromTrack, n.fix.Track)
	setNumber(tags, model.TagDiscNumber, n.fix.FromDisc, n.fix.Disc)
	md.tags = tags
	return md, n.correctionID, true
}

// setNumber sets a number tag ("3" or, keeping its total, "3/12") to the corrected value, unless
// the Correction leaves it as tagged.
func setNumber(tags model.Tags, tag model.TagName, from, to int) {
	if to == from || to <= 0 {
		return
	}
	v := strconv.Itoa(to)
	if old := tags[tag]; len(old) > 0 {
		if _, total, found := strings.Cut(old[0], "/"); found {
			v += "/" + total
		}
	}
	tags[tag] = []string{v}
}

// correctYear makes every date tag the File has a date in the year: a date in another year
// becomes just the year (its month and day are as doubtful), one in that year is kept whole. A
// File with no date gets the year as its date. So the album shows that one year in every client,
// whichever of its dates each uses.
func correctYear(tags model.Tags, year int) {
	found := false
	for _, t := range []model.TagName{model.TagRecordingDate, model.TagOriginalDate, model.TagReleaseDate} {
		v := tags[t]
		if len(v) == 0 || v[0] == "" {
			continue
		}
		found = true
		if Date(v[0]).Year() != year {
			tags[t] = []string{fmt.Sprintf("%04d", year)}
		}
	}
	if !found {
		tags[model.TagRecordingDate] = []string{fmt.Sprintf("%04d", year)}
	}
}

// IDMove says that what was attached to one ID (e.g. stars and play counts) belongs to another,
// because of the Correction with CorrectionID.
type IDMove struct {
	CorrectionID string
	From, To     string
	// Merge: To is an Artist of its own (an artist merge), so what is moved joins what To has, and
	// Undo moves back only what the merge moved in, not all of To's.
	Merge bool
	Undo  bool
}

// ArtistIDMoves returns where each renamed Artist's ID moves: from the old name's ID to the new
// one's, or back for a rename being undone. A rename that only changes capitals or spaces keeps
// the ID, so it moves nothing.
func (c Corrections) ArtistIDMoves() []IDMove {
	var moves []IDMove
	add := func(cr model.Correction, from, to string, undo bool) {
		m := IDMove{CorrectionID: cr.ID, From: ArtistID(from), To: ArtistID(to),
			Merge: cr.Kind == model.CorrectionArtistMerge, Undo: undo}
		if m.From != m.To {
			moves = append(moves, m)
		}
	}
	for _, cr := range c.renames {
		add(cr, cr.From, c.resolve(cr.To, nil), false)
	}
	for _, cr := range c.undoings {
		add(cr, c.resolve(cr.To, nil), cr.From, true)
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
// display names, adding them to the Track's CorrectionIDs. Other credits, like composers, keep the
// tagged names.
func (c Corrections) renameArtists(mf *model.MediaFile) {
	if len(c.renames) == 0 {
		return
	}
	applied := mf.CorrectionIDs
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
// A display name of just that Artist (give or take spaces) becomes the new name. Otherwise the
// name is replaced where it stands whole, so renaming "Ann" leaves "Joanne & Ann"'s Joanne alone.
func replaceName(display, name, to string) string {
	if model.ArtistNameKey(display) == model.ArtistNameKey(name) {
		return to
	}
	for start := 0; start < len(display); {
		i := strings.Index(display[start:], name)
		if i < 0 {
			break
		}
		i += start
		end := i + len(name)
		if !isWordRune(lastRune(display[:i])) && !isWordRune(firstRune(display[end:])) {
			return display[:i] + to + display[end:]
		}
		start = i + 1
	}
	return display
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}
