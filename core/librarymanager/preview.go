package librarymanager

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
)

// BulkEdit sets one field (a tagedit.Field key) to Values across the selected Tracks. No values
// clears the field; a multi-valued field (genres, artists) is set as the whole list.
type BulkEdit struct {
	Selection
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

// bulkFields are the fields a bulk edit can set: those the Tracks of an album, a folder or an
// Artist share. A Track's own fields (title, track number, lyrics, its recording IDs) are edited
// one Track at a time, and so are the totals, which a Track doesn't keep once scanned.
var bulkFields = []string{
	"artist", "artists", "album", "albumartist", "albumartists", "date", "originaldate",
	"discnumber", "genre", "compilation", "comment", "musicbrainz_albumid", "musicbrainz_releasegroupid",
}

// Fields are the fields a bulk edit can set, in the order the Edit dialog shows them.
func Fields() []tagedit.Field {
	var res []tagedit.Field
	for _, f := range tagedit.Fields {
		if slices.Contains(bulkFields, f.Key) {
			res = append(res, f)
		}
	}
	return res
}

// What a bulk edit does to a Track's File: StatusChange or StatusUnchanged, or the code
// (tagedit.Code) of the refusal it would meet, e.g. "unsupportedFormat".
const (
	StatusChange    = "change"
	StatusUnchanged = "unchanged"
)

// PreviewRow is one Track of a bulk edit's preview: its field's value as scanned, and as it
// would be written.
type PreviewRow struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Artist string   `json:"artist"`
	Album  string   `json:"album"`
	Path   string   `json:"path"`
	Old    []string `json:"old"`
	New    []string `json:"new"`
	Status string   `json:"status"`
}

// Preview is what a bulk edit would do. Total counts the selected Tracks, which are Changes,
// Unchanged or Refused (by code); Rows is a page of them, in path order.
type Preview struct {
	Field     string         `json:"field"`
	Values    []string       `json:"values"`
	Total     int            `json:"total"`
	Changes   int            `json:"changes"`
	Unchanged int            `json:"unchanged"`
	Refused   map[string]int `json:"refused"`
	Rows      []PreviewRow   `json:"rows"`
}

// Preview works out what the bulk edit would do to each selected Track, from its tags as scanned
// (the Files aren't read), and returns the rows from offset, at most limit of them. The same
// Tracks are what Run writes, if the Library doesn't change in between.
func (m *Manager) Preview(ctx context.Context, edit BulkEdit, offset, limit int) (*Preview, error) {
	rows, values, err := m.plan(ctx, edit)
	if err != nil {
		return nil, err
	}
	p := &Preview{Field: edit.Field, Values: values, Total: len(rows), Refused: map[string]int{}}
	for _, r := range rows {
		switch r.Status {
		case StatusChange:
			p.Changes++
		case StatusUnchanged:
			p.Unchanged++
		default:
			p.Refused[r.Status]++
		}
	}
	offset = min(max(offset, 0), len(rows))
	p.Rows = rows[offset:min(offset+max(limit, 0), len(rows))]
	return p, nil
}

// plan resolves the edit's Tracks, and what it would do to each.
func (m *Manager) plan(ctx context.Context, edit BulkEdit) ([]PreviewRow, []string, error) {
	if !slices.Contains(bulkFields, edit.Field) {
		return nil, nil, fmt.Errorf("%w: %q isn't edited in bulk", tagedit.ErrInvalidTags, edit.Field)
	}
	values, err := tagedit.Clean(edit.Field, edit.Values)
	if err != nil {
		return nil, nil, err
	}
	values = nonNil(values)
	mfs, err := m.Tracks(ctx, edit.Selection)
	if err != nil {
		return nil, nil, err
	}
	rows := make([]PreviewRow, len(mfs))
	for i := range mfs {
		mf := &mfs[i]
		old := scanned(mf, edit.Field)
		r := PreviewRow{ID: mf.ID, Title: mf.Title, Artist: mf.Artist, Album: mf.Album, Path: mf.Path,
			Old: old, New: values, Status: StatusChange}
		switch {
		case tagedit.CanEdit(mf) != nil:
			r.Status = tagedit.Code(tagedit.CanEdit(mf))
		case slices.Equal(old, values):
			r.Status = StatusUnchanged
		}
		rows[i] = r
	}
	return rows, values, nil
}

// scanned is the Track's value of a bulk field, as the scanner read it from the File: no values
// for a tag the File doesn't have (an album that reads as "[Unknown Album]", an album artist
// that only reads as the artist).
func scanned(mf *model.MediaFile, key string) []string {
	names := func(role model.Role) []string {
		var res []string
		for _, p := range mf.Participants[role] {
			res = append(res, p.Name)
		}
		return nonNil(res)
	}
	switch key {
	case "artist":
		return list(mf.Artist)
	case "artists":
		return names(model.RoleArtist)
	case "album":
		if mf.Album == consts.UnknownAlbum {
			return []string{}
		}
		return list(mf.Album)
	case "albumartist":
		if !mf.HasAlbumArtist {
			return []string{}
		}
		return list(mf.AlbumArtist)
	case "albumartists":
		if !mf.HasAlbumArtist {
			return []string{}
		}
		return names(model.RoleAlbumArtist)
	case "date":
		return list(mf.Date)
	case "originaldate":
		return list(mf.OriginalDate)
	case "discnumber":
		if mf.DiscNumber <= 0 {
			return []string{}
		}
		return list(strconv.Itoa(mf.DiscNumber))
	case "genre":
		return nonNil(slices.Clone(mf.Tags.Values(model.TagGenre)))
	case "compilation":
		if mf.Compilation {
			return []string{"1"}
		}
		return []string{}
	case "comment":
		return list(mf.Comment)
	case "musicbrainz_albumid":
		return list(mf.MbzAlbumID)
	case "musicbrainz_releasegroupid":
		return list(mf.MbzReleaseGroupID)
	}
	return []string{}
}

func list(v string) []string {
	if v == "" {
		return []string{}
	}
	return []string{v}
}

func nonNil(vs []string) []string {
	if vs == nil {
		return []string{}
	}
	return vs
}
