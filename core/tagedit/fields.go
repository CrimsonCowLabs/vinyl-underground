package tagedit

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// ErrInvalidTags is an edit with a field the dialog doesn't show, or a value no File should hold.
var ErrInvalidTags = errors.New("invalid tags")

// Values are a Track's editable fields by Field.Key. Every field is a list: a multi-valued one
// (genres, artists) can hold several values, and a File may hold several of any tag. No values
// means the tag isn't there (or, in an edit, that it is removed).
type Values map[string][]string

// FieldType is how a field's values are checked, and shown.
type FieldType string

const (
	TypeText     FieldType = "text"
	TypeLongText FieldType = "longtext" // lyrics, comments: newlines allowed
	TypeNumber   FieldType = "number"
	TypeDate     FieldType = "date"
	TypeBool     FieldType = "bool"
	TypeUUID     FieldType = "uuid"
)

// Field is one tag the Edit dialog shows: every tag the scanner maps (see resources/mappings.yaml),
// plus lyrics. Prop is its TagLib property, which TagLib maps to each format's own frame or atom.
type Field struct {
	Key   string    `json:"key"`
	Prop  string    `json:"-"`
	Type  FieldType `json:"type"`
	Multi bool      `json:"multi"`
}

// Track and disc numbers and their totals are one TagLib property each, "N/Total".
const (
	propTrack = "TRACKNUMBER"
	propDisc  = "DISCNUMBER"
)

// Fields are the editable fields, in the order the dialog shows them.
var Fields = []Field{
	{Key: "title", Prop: "TITLE", Type: TypeText},
	{Key: "artist", Prop: "ARTIST", Type: TypeText},
	{Key: "artists", Prop: "ARTISTS", Type: TypeText, Multi: true},
	{Key: "album", Prop: "ALBUM", Type: TypeText},
	{Key: "albumartist", Prop: "ALBUMARTIST", Type: TypeText},
	{Key: "albumartists", Prop: "ALBUMARTISTS", Type: TypeText, Multi: true},
	{Key: "date", Prop: "DATE", Type: TypeDate},
	{Key: "originaldate", Prop: "ORIGINALDATE", Type: TypeDate},
	{Key: "tracknumber", Prop: propTrack, Type: TypeNumber},
	{Key: "tracktotal", Prop: propTrack, Type: TypeNumber},
	{Key: "discnumber", Prop: propDisc, Type: TypeNumber},
	{Key: "disctotal", Prop: propDisc, Type: TypeNumber},
	{Key: "genre", Prop: "GENRE", Type: TypeText, Multi: true},
	{Key: "compilation", Prop: "COMPILATION", Type: TypeBool},
	{Key: "bpm", Prop: "BPM", Type: TypeNumber},
	{Key: "comment", Prop: "COMMENT", Type: TypeLongText},
	{Key: "lyrics", Prop: "LYRICS", Type: TypeLongText},
	{Key: "musicbrainz_trackid", Prop: "MUSICBRAINZ_TRACKID", Type: TypeUUID},
	{Key: "musicbrainz_releasetrackid", Prop: "MUSICBRAINZ_RELEASETRACKID", Type: TypeUUID},
	{Key: "musicbrainz_albumid", Prop: "MUSICBRAINZ_ALBUMID", Type: TypeUUID},
	{Key: "musicbrainz_artistid", Prop: "MUSICBRAINZ_ARTISTID", Type: TypeUUID, Multi: true},
	{Key: "musicbrainz_albumartistid", Prop: "MUSICBRAINZ_ALBUMARTISTID", Type: TypeUUID, Multi: true},
	{Key: "musicbrainz_releasegroupid", Prop: "MUSICBRAINZ_RELEASEGROUPID", Type: TypeUUID},
}

func field(key string) (Field, bool) {
	i := slices.IndexFunc(Fields, func(f Field) bool { return f.Key == key })
	if i < 0 {
		return Field{}, false
	}
	return Fields[i], true
}

// numberPair splits an "N/Total" property into its number and total.
func numberPair(vs []string) (n, total string) {
	if len(vs) == 0 {
		return "", ""
	}
	n, total, _ = strings.Cut(vs[0], "/")
	return strings.TrimSpace(n), strings.TrimSpace(total)
}

func list(v string) []string {
	if v == "" {
		return nil
	}
	return []string{v}
}

// valuesOf maps a File's TagLib properties to the editable fields.
func valuesOf(props map[string][]string) Values {
	vals := Values{}
	for _, f := range Fields {
		switch f.Key {
		case "tracknumber", "discnumber":
			n, _ := numberPair(props[f.Prop])
			vals[f.Key] = list(n)
		case "tracktotal", "disctotal":
			_, t := numberPair(props[f.Prop])
			vals[f.Key] = list(t)
		default:
			vals[f.Key] = slices.Clone(props[f.Prop])
		}
		if vals[f.Key] == nil {
			vals[f.Key] = []string{}
		}
	}
	return vals
}

var (
	numberRe = regexp.MustCompile(`^\d{1,6}$`)
	dateRe   = regexp.MustCompile(`^\d{4}(-\d{2}(-\d{2})?)?$`)
	uuidRe   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// maxValueLen bounds one value; lyrics may be long.
const (
	maxValueLen  = 4096
	maxLyricsLen = 256 * 1024
)

// clean trims an edit's values, drops empty ones, and checks them. A single-valued field holds
// one value, unless many (the File holds several already).
func clean(f Field, vs []string, many bool) ([]string, error) {
	var res []string
	for _, v := range vs {
		if f.Type != TypeLongText {
			v = strings.TrimSpace(v)
		} else {
			v = strings.TrimRight(strings.ReplaceAll(v, "\r\n", "\n"), " \t\n")
		}
		if v == "" {
			continue
		}
		limit := maxValueLen
		if f.Key == "lyrics" {
			limit = maxLyricsLen
		}
		if len(v) > limit {
			return nil, fmt.Errorf("%w: %s is too long", ErrInvalidTags, f.Key)
		}
		if strings.ContainsFunc(v, func(r rune) bool {
			return unicode.IsControl(r) && (f.Type != TypeLongText || (r != '\n' && r != '\t'))
		}) {
			return nil, fmt.Errorf("%w: %s holds a control character", ErrInvalidTags, f.Key)
		}
		var ok bool
		switch f.Type {
		case TypeNumber:
			ok = numberRe.MatchString(v)
		case TypeDate:
			ok = dateRe.MatchString(v)
		case TypeBool:
			ok = v == "0" || v == "1"
		case TypeUUID:
			ok = uuidRe.MatchString(v)
		default:
			ok = true
		}
		if !ok {
			return nil, fmt.Errorf("%w: %s can't be %q", ErrInvalidTags, f.Key, v)
		}
		res = append(res, v)
	}
	if len(res) > 1 && !f.Multi && f.Type != TypeLongText && !many {
		return nil, fmt.Errorf("%w: %s holds one value", ErrInvalidTags, f.Key)
	}
	return res, nil
}

// changes works out the TagLib properties to write for an edit of a File whose properties are
// cur: only the fields whose values differ from the File's, so a tag the admin left alone is
// never rewritten. A number keeps its total (and the other way round) when only one changes. No
// values removes the property.
func changes(cur map[string][]string, edit Values) (map[string][]string, error) {
	have := valuesOf(cur)
	want := Values{}
	for k, vs := range edit {
		f, ok := field(k)
		if !ok {
			return nil, fmt.Errorf("%w: no field %q", ErrInvalidTags, k)
		}
		// A File may hold several values of a single-valued field (e.g. two ARTIST frames): an
		// edit may keep them.
		vs, err := clean(f, vs, len(have[k]) > 1)
		if err != nil {
			return nil, err
		}
		if !slices.Equal(vs, have[k]) {
			want[k] = vs
		}
	}
	set := map[string][]string{}
	for k, vs := range want {
		f, _ := field(k)
		if f.Prop != propTrack && f.Prop != propDisc {
			set[f.Prop] = vs
		}
	}
	for _, pair := range [][3]string{{propTrack, "tracknumber", "tracktotal"}, {propDisc, "discnumber", "disctotal"}} {
		prop, nKey, tKey := pair[0], pair[1], pair[2]
		_, nChanged := want[nKey]
		_, tChanged := want[tKey]
		if !nChanged && !tChanged {
			continue
		}
		n, t := first(have[nKey]), first(have[tKey])
		if nChanged {
			n = first(want[nKey])
		}
		if tChanged {
			t = first(want[tKey])
		}
		switch {
		case n == "" && t != "":
			return nil, fmt.Errorf("%w: %s needs a number to go with its total", ErrInvalidTags, nKey)
		case t != "":
			set[prop] = []string{n + "/" + t}
		default:
			set[prop] = list(n)
		}
	}
	return set, nil
}

func first(vs []string) string {
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}
