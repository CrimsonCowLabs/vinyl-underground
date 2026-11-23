package persistence

import (
	"fmt"
	"strings"

	. "github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/consts"
)

// The Library manager's filters on the Track listing (#166), by their REST filter names. They
// combine with each other and with the listing's other filters (artists_id, album_id, title for
// free text, library_id, missing). Each is a plain condition on a media_file column, so the
// Library manager pages over the whole Library from the server.
func libraryManagerFilters() map[string]filterFunc {
	return map[string]filterFunc{
		"folder":      folderFilter,
		"format":      formatFilter,
		"rip":         ripFilter,
		"missing_tag": missingTagFilter,
	}
}

// folderFilter matches the Files inside a folder (a path relative to its library), and in its
// subfolders, ignoring the letter case. It is a range over the path, not a LIKE, so a folder
// whose name holds a LIKE wildcard ("CD_Rips") matches only itself, and the media_file_path_nocase
// index is used. "Album 1" doesn't match "Album 10": the range ends at the folder's own slash.
func folderFilter(_ string, value any) Sqlizer {
	folder := strings.Trim(fmt.Sprint(value), "/")
	if folder == "" {
		return nil
	}
	// Every path inside the folder starts with "<folder>/", and sorts before "<folder>0" ('0' is
	// the character after '/').
	return Expr("(media_file.path >= ? collate nocase and media_file.path < ? collate nocase)",
		folder+"/", folder+"0")
}

// formatFilter matches the Files of a format, by their suffix ("mp3", "flac"), in any case.
func formatFilter(_ string, value any) Sqlizer {
	format := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(fmt.Sprint(value)), "."))
	if format == "" {
		return nil
	}
	return Eq{"lower(media_file.suffix)": format}
}

// isRip is a Rip (see GLOSSARY.md): a File whose SOURCEURL is an http(s) URL. SOURCE alone
// (e.g. "CD") doesn't make one.
const isRip = "(media_file.source_url like 'http://_%' or media_file.source_url like 'https://_%')"

// ripFilter matches the Rips ("true"), or every File but them ("false").
func ripFilter(_ string, value any) Sqlizer {
	switch strings.ToLower(fmt.Sprint(value)) {
	case "true":
		return Expr(isRip)
	case "false":
		return Expr("not " + isRip)
	}
	return nil
}

// missingTags are the tags the Library manager finds Files without, by the missing_tag filter's
// values. Each is how a missing tag reads in the Track: no album reads as "[Unknown Album]", no
// year (no date the scanner could read) and no track number as 0, and no album artist tag only
// shows in has_album_artist, as album_artist falls back to the artist.
var missingTags = map[string]Sqlizer{
	"album":        Or{Eq{"media_file.album": ""}, Eq{"media_file.album": consts.UnknownAlbum}},
	"album_artist": Eq{"media_file.has_album_artist": false},
	"year":         Eq{"media_file.year": 0},
	"track_number": Eq{"media_file.track_number": 0},
}

// missingTagFilter matches the Files missing every one of the given tags (a value of missingTags,
// or a list of them). An unknown tag matches nothing.
func missingTagFilter(_ string, value any) Sqlizer {
	var tags []string
	switch v := value.(type) {
	case []string:
		tags = v
	case []any:
		for _, t := range v {
			tags = append(tags, fmt.Sprint(t))
		}
	default:
		tags = strings.Split(fmt.Sprint(v), ",")
	}
	cond := And{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		c, ok := missingTags[t]
		if !ok {
			return Expr("1 = 0")
		}
		cond = append(cond, c)
	}
	if len(cond) == 0 {
		return nil
	}
	return cond
}
