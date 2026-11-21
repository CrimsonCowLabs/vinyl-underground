package rip

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"go.senan.xyz/taglib"
)

// ErrInvalidTags is returned for an edit with a value no File should hold: a control character
// (which would split one tag into several), a year that isn't a year, or a track number that
// isn't a number.
var ErrInvalidTags = errors.New("invalid tags")

// maxTagLen bounds any one edited value.
const maxTagLen = 1024

// TagEdit is what the review card changes in one File. A nil field leaves that tag as it is, and
// an empty one removes it. Every other tag in the File, SOURCE and SOURCEURL included, is kept.
type TagEdit struct {
	Title       *string `json:"title,omitempty"`
	Artist      *string `json:"artist,omitempty"`
	Album       *string `json:"album,omitempty"`
	AlbumArtist *string `json:"albumArtist,omitempty"`
	// Year is written as the File's DATE.
	Year *string `json:"year,omitempty"`
	// TrackNumber is the Track's number on its album, without the total.
	TrackNumber *string `json:"trackNumber,omitempty"`
}

// FileTags are the tags of one File the review card shows. Year is DATE's year, and TrackNumber
// TRACKNUMBER without its total.
type FileTags struct {
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	AlbumArtist string `json:"albumArtist"`
	Year        string `json:"year"`
	TrackNumber string `json:"trackNumber"`
}

// tagWriter writes tags into the File at path, keeping the tags it isn't given.
type tagWriter func(path string, tags map[string][]string) error

func writeTagsKeepingOthers(path string, tags map[string][]string) error {
	return taglib.WriteTags(path, tags, 0)
}

var (
	yearRe  = regexp.MustCompile(`^\d{1,4}$`)
	trackRe = regexp.MustCompile(`^\d{1,4}$`)
)

// validate trims the edit's values and checks them.
func (e *TagEdit) validate() error {
	for _, f := range []**string{&e.Title, &e.Artist, &e.Album, &e.AlbumArtist, &e.Year, &e.TrackNumber} {
		if *f == nil {
			continue
		}
		v := strings.TrimSpace(**f)
		*f = &v
		if len(v) > maxTagLen || strings.ContainsFunc(v, unicode.IsControl) {
			return fmt.Errorf("%w: %q", ErrInvalidTags, v)
		}
	}
	if e.Year != nil && *e.Year != "" && !yearRe.MatchString(*e.Year) {
		return fmt.Errorf("%w: the year must be a number, not %q", ErrInvalidTags, *e.Year)
	}
	if e.TrackNumber != nil && *e.TrackNumber != "" && !trackRe.MatchString(*e.TrackNumber) {
		return fmt.Errorf("%w: the track number must be a number, not %q", ErrInvalidTags, *e.TrackNumber)
	}
	return nil
}

// readFileTags reads the tags the review card shows from the File at path.
func readFileTags(path string) (FileTags, error) {
	tags, err := taglib.ReadTags(path)
	if err != nil {
		return FileTags{}, fmt.Errorf("reading the tags of %s: %w", path, err)
	}
	return fileTags(tags), nil
}

func fileTags(tags map[string][]string) FileTags {
	first := func(k string) string {
		if vs := tags[k]; len(vs) > 0 {
			return strings.Join(vs, "; ")
		}
		return ""
	}
	return FileTags{
		Title:       first(taglib.Title),
		Artist:      first(taglib.Artist),
		Album:       first(taglib.Album),
		AlbumArtist: first(taglib.AlbumArtist),
		Year:        yearOf(first(taglib.Date)),
		TrackNumber: trackOf(first(taglib.TrackNumber)),
	}
}

// yearOf is the year a DATE starts with ("2001" for "2001-05-03").
func yearOf(date string) string {
	date = strings.TrimSpace(date)
	end := strings.IndexFunc(date, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		return date
	}
	return date[:end]
}

// trackOf is a TRACKNUMBER without its total ("2" for "2/12").
func trackOf(n string) string {
	n, _, _ = strings.Cut(strings.TrimSpace(n), "/")
	return strings.TrimSpace(n)
}

// changes works out which tags the edit really changes in a File whose tags are cur, so tags the
// admin left alone are not rewritten, and a DATE or TRACKNUMBER keeps what the card doesn't show
// (the full date, the track total) when the year or number is unchanged.
func (e TagEdit) changes(cur map[string][]string) map[string][]string {
	shown := fileTags(cur)
	set := map[string][]string{}
	put := func(key string, v *string, current string, raw func(string) string) {
		if v == nil || *v == current {
			return
		}
		if *v == "" {
			set[key] = nil
			return
		}
		set[key] = []string{raw(*v)}
	}
	same := func(v string) string { return v }
	put(taglib.Title, e.Title, shown.Title, same)
	put(taglib.Artist, e.Artist, shown.Artist, same)
	put(taglib.Album, e.Album, shown.Album, same)
	put(taglib.AlbumArtist, e.AlbumArtist, shown.AlbumArtist, same)
	put(taglib.Date, e.Year, shown.Year, same)
	put(taglib.TrackNumber, e.TrackNumber, shown.TrackNumber, func(n string) string {
		if vs := cur[taglib.TrackNumber]; len(vs) > 0 {
			if _, total, ok := strings.Cut(vs[0], "/"); ok && strings.TrimSpace(total) != "" {
				return n + "/" + strings.TrimSpace(total)
			}
		}
		return n
	})
	return set
}

// editTags writes the edit into the File at path, and reports whether anything changed. It never
// writes the File in place: it writes a hidden copy next to it (which the scanner skips) and
// renames the copy over the File only once the write succeeded, so a failed write leaves the File
// as it was. A nil write uses TagLib.
func editTags(path string, edit TagEdit, write tagWriter) (bool, error) {
	if err := edit.validate(); err != nil {
		return false, err
	}
	if write == nil {
		write = writeTagsKeepingOthers
	}
	cur, err := taglib.ReadTags(path)
	if err != nil {
		return false, fmt.Errorf("reading the tags of %s: %w", path, err)
	}
	set := edit.changes(cur)
	if len(set) == 0 {
		return false, nil
	}

	tmp, err := copyBeside(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(tmp) }() // a no-op once it is renamed into place
	if err := write(tmp, set); err != nil {
		return false, fmt.Errorf("writing tags to %s: %w", path, err)
	}
	// Make sure what was written still reads as an audio File before it replaces the original.
	if _, err := taglib.ReadTags(tmp); err != nil {
		return false, fmt.Errorf("the File with new tags doesn't read back: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return false, fmt.Errorf("replacing %s: %w", path, err)
	}
	return true, nil
}

// copyBeside copies the File at path into a new hidden File in the same folder, with the same
// extension (TagLib picks the format by it) and permissions, and returns the copy's path.
func copyBeside(path string) (string, error) {
	src, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return "", err
	}
	ext := filepath.Ext(path)
	base := truncate(strings.TrimSuffix(filepath.Base(path), ext), 100)
	dst, err := os.CreateTemp(filepath.Dir(path), "."+base+".edit-*"+ext)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(dst, src)
	err = errors.Join(err, dst.Chmod(info.Mode().Perm()), dst.Sync(), dst.Close())
	if err != nil {
		_ = os.Remove(dst.Name())
		return "", fmt.Errorf("copying %s: %w", path, err)
	}
	return dst.Name(), nil
}
