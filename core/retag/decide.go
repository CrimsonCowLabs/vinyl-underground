package retag

import (
	"fmt"
	"strconv"

	"github.com/navidrome/navidrome/core/acoustid"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/utils/str"
)

// minAcoustIDScore is how sure AcoustID must be that a fingerprint is a recording's.
const minAcoustIDScore = 0.9

// Want is what a File's tags should be: its values after the local fixes, repaired from a data
// source. A data source only fills what is empty and replaces what is flagged as junk; a value
// that is set and looks right is kept, and a difference is noted.
type Want struct {
	Local
	AlbumArtist string
	// IDs are the MusicBrainz IDs to add, by tagedit field key.
	IDs map[string][]string
}

func (w *Want) setID(key string, vs ...string) {
	if len(vs) == 0 || vs[0] == "" {
		return
	}
	if w.IDs == nil {
		w.IDs = map[string][]string{}
	}
	w.IDs[key] = vs
}

// betterTitle is the title of a File, given that a data source names it title: the source's
// for a File with no real title or one made from its name, and for a kept number when the
// source's title has none.
func betterTitle(f File, cur, title string) string {
	switch {
	case title == "":
		return cur
	case f.Has(FlagNoTitle), f.Has(FlagSlug):
		return title
	case f.Has(FlagNumberKept) && similarity(f.Alt, title) > similarity(f.Title, title):
		return title
	}
	return cur
}

// fromRelease repairs the File from the release track it was placed on. whole says every File
// of its folder was placed on the release: only then are the album's IDs added, as an album
// whose Files have different album IDs would be split in two.
func (w *Want) fromRelease(f File, p Placed, rel *musicbrainz.Release, whole bool) (notes []string) {
	t := p.Track
	w.Title = betterTitle(f, w.Title, t.Title)
	artist := t.ArtistCredit
	if len(artist) == 0 {
		artist = rel.ArtistCredit
	}
	switch {
	case f.Has(FlagUnknownArtist):
		w.Artist = artist.String()
	case artist.String() != "" && similarity(w.Artist, artist.String()) < titleThreshold:
		notes = append(notes, fmt.Sprintf("kept artist %q; MusicBrainz credits %q", w.Artist, artist.String()))
	}
	if f.Has(FlagNoAlbum) {
		w.Album, w.AlbumArtist = rel.Title, rel.ArtistCredit.String()
	}
	switch {
	case w.Track == 0:
		w.Track = t.Position
	case w.Track != t.Position:
		notes = append(notes, fmt.Sprintf("kept track number %d; MusicBrainz has %d", w.Track, t.Position))
	}
	// A folder may number a two-disc album's tracks straight through: its disc is only the
	// release's where its track number is too.
	if w.Disc == 0 && len(rel.Media) > 1 && w.Track == t.Position {
		w.Disc = p.Disc
	}
	year := atoi(yearOf(rel.ReleaseGroup.FirstReleaseDate))
	if year == 0 {
		year = atoi(rel.Year())
	}
	switch {
	case year == 0:
	case w.Year == 0:
		w.Year = year
	case w.Year != year:
		notes = append(notes, fmt.Sprintf("kept year %d; MusicBrainz has %d", w.Year, year))
	}
	if f.MbzRecordingID == "" && f.MbzAlbumID == "" {
		w.setID("musicbrainz_trackid", t.Recording.ID)
		w.setID("musicbrainz_releasetrackid", t.ID)
		w.setID("musicbrainz_artistid", artist.IDs()...)
		if whole {
			w.setID("musicbrainz_albumid", rel.ID)
			w.setID("musicbrainz_releasegroupid", rel.ReleaseGroup.ID)
			w.setID("musicbrainz_albumartistid", rel.ArtistCredit.IDs()...)
		}
	}
	return notes
}

// fromAcoustID repairs the File from AcoustID's matches of its fingerprint, and reports whether
// one was taken: the best match must be sure, and name one recording (or, for a File with a
// real title, have one with that title).
func (w *Want) fromAcoustID(f File, results []acoustid.Result) (notes []string, ok bool) {
	if len(results) == 0 || results[0].Score < minAcoustIDScore {
		return []string{"AcoustID has no sure match"}, false
	}
	var recs []acoustid.Recording
	for _, r := range results[0].Recordings {
		if r.Title != "" {
			recs = append(recs, r)
		}
	}
	if len(recs) == 0 {
		return []string{"AcoustID's match names no recording"}, false
	}
	rec := recs[0]
	junk := f.Has(FlagNoTitle) || f.Has(FlagSlug)
	if !junk {
		best := -1.0
		for _, r := range recs {
			if s := max(similarity(f.Title, r.Title), similarity(f.Alt, r.Title)); s > best {
				best, rec = s, r
			}
		}
		if best < titleThreshold && !f.Has(FlagNumberKept) {
			return []string{fmt.Sprintf("AcoustID names %q, not %q", rec.Title, f.Title)}, false
		}
	} else {
		for _, r := range recs[1:] {
			if str.SanitizeFieldForSorting(r.Title) != str.SanitizeFieldForSorting(rec.Title) {
				return []string{fmt.Sprintf("AcoustID names more than one recording (%q, %q)", rec.Title, r.Title)}, false
			}
		}
	}
	w.Title = betterTitle(f, w.Title, rec.Title)
	if f.Has(FlagUnknownArtist) && rec.Artist() != "" {
		w.Artist = rec.Artist()
	}
	if f.Has(FlagNoAlbum) {
		for _, rg := range rec.ReleaseGroups {
			if rg.Type == "Album" && len(rg.SecondaryTypes) == 0 {
				w.Album = rg.Title
				break
			}
		}
	}
	if f.MbzRecordingID == "" {
		w.setID("musicbrainz_trackid", rec.ID)
		w.setID("musicbrainz_artistid", rec.ArtistIDs()...)
	}
	return nil, true
}

func yearOf(date string) string { return date[:min(len(date), 4)] }

// values are the tagedit fields to write for the File whose values are orig: only those that
// change.
func (w Want) values(orig Local) tagedit.Values {
	v := tagedit.Values{}
	text := func(key, was, now string) {
		if now != was && now != "" {
			v[key] = []string{now}
		}
	}
	number := func(key string, was, now int) {
		if now != was && now > 0 {
			v[key] = []string{strconv.Itoa(now)}
		}
	}
	text("title", orig.Title, w.Title)
	text("artist", orig.Artist, w.Artist)
	text("album", orig.Album, w.Album)
	if w.AlbumArtist != "" {
		v["albumartist"] = []string{w.AlbumArtist}
	}
	number("tracknumber", orig.Track, w.Track)
	number("discnumber", orig.Disc, w.Disc)
	if orig.Year == 0 {
		number("date", orig.Year, w.Year)
	}
	for k, ids := range w.IDs {
		v[k] = ids
	}
	return v
}
