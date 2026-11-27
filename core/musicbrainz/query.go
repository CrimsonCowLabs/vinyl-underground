package musicbrainz

import (
	"regexp"
	"strings"
	"unicode"
)

// Find on MusicBrainz searches by what the admin types into its search field: the album's title
// to begin with, but a title MusicBrainz spells otherwise, or a compilation among a hundred of
// the same name, is found sooner by its barcode, its catalog number or a link to the release.
// ParseQuery works out which of them was typed.

// QueryKind is what a search field's text is taken for.
type QueryKind int

const (
	// QueryTitle is a release title: searched as one (with the album artist), then, if that finds
	// nothing, as a catalog number.
	QueryTitle QueryKind = iota
	// QueryCatalogNumber looks like a catalog number ("WARPCD92", "TVT-4010-2"): searched as one,
	// then, if that finds nothing, as a title.
	QueryCatalogNumber
	// QueryBarcode is 8 to 14 digits (UPC-A, EAN-13, EAN-8...), with any spaces or hyphens taken
	// out: searched as a barcode, then, if that finds nothing, as a catalog number as typed (many
	// look like "76869-75027-2").
	QueryBarcode
	// QueryRelease is a release's MBID, bare or in a link to its MusicBrainz page: looked up, with
	// no search.
	QueryRelease
	// QueryOtherLink is any other link (a release group's page, Discogs...): nothing to search by.
	QueryOtherLink
)

// Query is a search field's text, as ParseQuery takes it.
type Query struct {
	Kind QueryKind
	// Value is what is searched by: the release's MBID (lower case), the barcode's digits, or the
	// text as typed (trimmed).
	Value string
	// Text is the text as typed (trimmed), for the searches tried when the first finds nothing.
	Text string
}

var (
	// releaseLink is a link to a release's page on MusicBrainz (or its beta), with or without
	// its scheme, and anything after the MBID (/cover-art, ?tab=...).
	releaseLink = regexp.MustCompile(`(?i)^(?:https?://)?(?:[a-z0-9-]+\.)?musicbrainz\.org/release/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:[/?#].*)?$`)
	bareMBID    = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	otherLink   = regexp.MustCompile(`(?i)^(?:https?://|www\.)\S+$`)
	// barcodeText is digits, perhaps broken up by spaces or hyphens.
	barcodeText = regexp.MustCompile(`^[0-9]+(?:[ -]+[0-9]+)*$`)
)

// ParseQuery works out what the search field's text is: a release link or MBID, a link to
// anything else, a barcode, something like a catalog number, or a title.
//
// A barcode is 8 to 14 digits, spaces or hyphens allowed. A catalog number is one word (no
// spaces) with at least two digits and something other than digits in it: "WARPCD92",
// "TVT-4010-2". Titles like that are few ("4:44"), and as the catalog number search is followed
// by the title one when it finds nothing, they are still found. A word of digits alone ("1999")
// is a title. Anything else is a title, tried as a catalog number when it finds nothing.
func ParseQuery(text string) Query {
	text = strings.TrimSpace(text)
	q := Query{Kind: QueryTitle, Value: text, Text: text}
	switch {
	case releaseLink.MatchString(text):
		q.Kind, q.Value = QueryRelease, strings.ToLower(releaseLink.FindStringSubmatch(text)[1])
	case bareMBID.MatchString(text):
		q.Kind, q.Value = QueryRelease, strings.ToLower(text)
	case otherLink.MatchString(text):
		q.Kind = QueryOtherLink
	case barcodeText.MatchString(text):
		if digits := strings.NewReplacer(" ", "", "-", "").Replace(text); len(digits) >= 8 && len(digits) <= 14 {
			q.Kind, q.Value = QueryBarcode, digits
		}
	case looksLikeCatalogNumber(text):
		q.Kind = QueryCatalogNumber
	}
	return q
}

// looksLikeCatalogNumber is true for one word with at least two digits and something other than
// a digit.
func looksLikeCatalogNumber(text string) bool {
	digits, others := 0, 0
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			return false
		case unicode.IsDigit(r):
			digits++
		default:
			others++
		}
	}
	return digits >= 2 && others > 0
}

// VariousArtistsName is MusicBrainz's name for the credit of most compilations.
const VariousArtistsName = "Various Artists"

// variousArtistsNames are the names libraries give Various Artists, folded to lower case with
// single spaces.
var variousArtistsNames = map[string]bool{
	"various": true, "various artist": true, "various artists": true,
	"va": true, "v.a.": true, "v.a": true, "v/a": true,
}

// IsVariousArtistsName reports whether an album artist's name means Various Artists: "Various",
// "VA", "V.A.", "V/A", "Various Artist(s)", in any case.
func IsVariousArtistsName(name string) bool {
	return variousArtistsNames[strings.ToLower(strings.Join(strings.Fields(name), " "))]
}

// ReleaseSearchArtist is the artist to search a release by for an album: MusicBrainz credits
// compilations to "Various Artists", which a library may call "Various" or "VA", so a compilation,
// or an album by any of those names, is searched as Various Artists.
func ReleaseSearchArtist(albumArtist string, compilation bool) string {
	if compilation || IsVariousArtistsName(albumArtist) {
		return VariousArtistsName
	}
	return strings.TrimSpace(albumArtist)
}
