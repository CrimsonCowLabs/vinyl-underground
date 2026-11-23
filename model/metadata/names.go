package metadata

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/navidrome/navidrome/model"
)

// ArtistID returns the ID of the Artist with the given name.
func ArtistID(name string) string {
	return Metadata{}.artistID(name)
}

// ReplaceName swaps the name for its new one in a display name, which joins the tagged names
// (e.g. a Track artist "Ann & Bob"): an artist rename written into a File (see editjob.Rename).
// A display name of just that Artist (give or take spaces) becomes the new name. Otherwise the
// name is replaced where it stands whole, so renaming "Ann" leaves "Joanne & Ann"'s Joanne alone.
func ReplaceName(display, name, to string) string {
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
