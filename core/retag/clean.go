// Package retag repairs the tags of the Library's Files in bulk, with no review (the `retag`
// command): titles holding their track number, file extensions, artists and years, and gaps.
// Local evidence (the title, the File's name and folder) fixes what it can settle; MusicBrainz,
// and AcoustID fingerprints, settle the rest, or the File is reported and only its safe fixes
// are made. Every write goes through tagedit's write path and the Edit journal.
package retag

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Local is what is known about a File without asking anyone: its tags, and its name and folder.
type Local struct {
	Title, Artist, Album string
	Track, Disc, Year    int
	// Stem is the File's name without its extension; Folder the name of the folder it is in.
	Stem, Folder string
}

// Flag is something about a File local evidence couldn't settle, for a data source to.
type Flag string

const (
	// FlagNoTitle is a title that is only a number, or "Track NN": the real title isn't known.
	FlagNoTitle Flag = "no-title"
	// FlagNumberKept is a title starting with a number that may be its track number, or part of
	// the title ("12 Fake Plastic Trees", "7 Seconds"); Cleaned.Alt is the title without it.
	FlagNumberKept Flag = "number-kept"
	// FlagSlug is a title made from the File's name ("Rosa_Parks", "sublime-sinsemilla-romeo").
	FlagSlug Flag = "slug"
	// FlagUnknownArtist is an artist that is empty or "[Unknown Artist]".
	FlagUnknownArtist Flag = "unknown-artist"
	// FlagNoAlbum is an album that is empty or "Unknown Album".
	FlagNoAlbum Flag = "no-album"
)

// Cleaned is a File's Local values after the local fixes.
type Cleaned struct {
	Local
	// Fixes say what changed, for the report.
	Fixes []string
	Flags []Flag
	// Alt is the title without its leading number, with FlagNumberKept.
	Alt string
}

// Has reports whether the File has the flag.
func (c Cleaned) Has(f Flag) bool {
	for _, g := range c.Flags {
		if g == f {
			return true
		}
	}
	return false
}

var (
	spacesRe = regexp.MustCompile(`\s{2,}`)
	extRe    = regexp.MustCompile(`(?i)\.(mp3|m4a|aac|flac|wav|aiff?|ogg|opus|wma)$`)
	// "05. Toni Basil - Mickey (1982)", "14- Dmx - Pull Out (Skit)"
	numberedFullRe = regexp.MustCompile(`^(\d{1,3})\s*[-.]\s*(.+?)\s+-\s+(.+?)(?:\s*\(((?:19|20)\d{2})\))?$`)
	// "09 of 11 - The Sworn Sword": a part of an audiobook, not a track number.
	partOfRe = regexp.MustCompile(`(?i)^of\s+\d`)
	// "5-15 After da Hour", "1-07 17": a disc and a track number.
	discTrackRe = regexp.MustCompile(`^(\d{1,2})-(\d{2})\s+(.+)$`)
	// "10 - Bones", "03. Airbag", "04_Lucky", "01 Halloween", "12 Fake Plastic Trees".
	trackPrefixRe = regexp.MustCompile(`^(\d{1,3})(\s*[-–.]\s*|_|\s+)(.+)$`)
	trackNNRe     = regexp.MustCompile(`(?i)^track\s*(\d{1,3})$`)
	onlyNumberRe  = regexp.MustCompile(`^\d+$`)
	// In a File's name: "2-03 Foo", "07 Foo", "07. Foo", "7 - Foo", "07_Foo".
	stemDiscTrackRe = regexp.MustCompile(`^(\d{1,2})-(\d{2})[\s._-]`)
	stemTrackRe     = regexp.MustCompile(`^(?:(\d{1,3})\s*[-.]\s*|(\d{1,3})_|(0\d)\s)`)
	discFolderRe    = regexp.MustCompile(`(?i)^(?:cd|disc|disk)\s*(\d{1,2})$`)
	yearFolderRe    = regexp.MustCompile(`\(((?:19|20)\d{2})\)`)
)

// Clean makes the fixes local evidence settles, and flags the rest. Cleaning a cleaned File
// changes nothing.
func Clean(in Local) Cleaned {
	c := Cleaned{Local: in}
	c.Title = squeeze(c.Title)
	c.Artist = squeeze(c.Artist)
	c.Album = squeeze(c.Album)
	if c.Title != in.Title || c.Artist != in.Artist || c.Album != in.Album {
		c.fix("trimmed spaces")
	}
	if t := strings.TrimSpace(extRe.ReplaceAllString(c.Title, "")); t != c.Title && t != "" {
		c.Title = t
		c.fix("dropped a file extension from the title")
	}
	fromName := c.Title == squeeze(extRe.ReplaceAllString(in.Stem, ""))

	if unknownArtist(c.Artist) {
		if m := numberedFullRe.FindStringSubmatch(c.Title); m != nil && c.trackFits(atoi(m[1])) {
			c.setTrack(atoi(m[1]))
			c.Artist, c.Title = m[2], m[3]
			if m[4] != "" && c.Year == 0 {
				c.Year = atoi(m[4])
			}
			c.fix("split the artist, track number and year out of the title")
		}
	}
	c.stripNumber()
	c.stripArtist()
	if fromName {
		c.unslug()
	}

	if m := trackNNRe.FindStringSubmatch(c.Title); m != nil {
		if c.Track == 0 {
			c.setTrack(atoi(m[1]))
		}
		c.flag(FlagNoTitle)
	} else if onlyNumberRe.MatchString(c.Title) || c.Title == "" {
		c.flag(FlagNoTitle)
	}

	c.fillFromName()
	if unknownArtist(c.Artist) {
		c.flag(FlagUnknownArtist)
	}
	if unknownAlbum(c.Album) {
		c.flag(FlagNoAlbum)
	}
	return c
}

func (c *Cleaned) fix(s string) { c.Fixes = append(c.Fixes, s) }

func (c *Cleaned) flag(f Flag) {
	if !c.Has(f) {
		c.Flags = append(c.Flags, f)
	}
}

// trackFits reports whether n can be the File's track number: it has none, or n.
func (c *Cleaned) trackFits(n int) bool { return n > 0 && (c.Track == 0 || c.Track == n) }

func (c *Cleaned) setTrack(n int) {
	if c.Track == 0 {
		c.Track = n
		c.fix("set the track number")
	}
}

// stripNumber takes a track number off the front of the title, when it is one: the File has no
// other track number, and the number is set apart (by punctuation, or written with a leading
// zero). A number only followed by a space may be part of the title, so it is kept and flagged.
func (c *Cleaned) stripNumber() {
	if m := discTrackRe.FindStringSubmatch(c.Title); m != nil {
		d, n := atoi(m[1]), atoi(m[2])
		if d > 0 && (c.Disc == 0 || c.Disc == d) && c.trackFits(n) {
			if c.Disc == 0 {
				c.Disc = d
				c.fix("set the disc number")
			}
			c.setTrack(n)
			c.Title = m[3]
			c.fix("took the disc and track number off the title")
			return
		}
	}
	m := trackPrefixRe.FindStringSubmatch(c.Title)
	if m == nil || !hasLetter(m[3]) || partOfRe.MatchString(m[3]) {
		return
	}
	n := atoi(m[1])
	if !c.trackFits(n) {
		return
	}
	setApart := strings.TrimSpace(m[2]) != "" || (len(m[1]) >= 2 && m[1][0] == '0')
	if !setApart {
		c.Alt = m[3]
		c.flag(FlagNumberKept)
		return
	}
	c.setTrack(n)
	c.Title = m[3]
	c.fix("took the track number off the title")
}

// stripArtist takes "Artist - " off the front of the title, when it is the File's artist.
func (c *Cleaned) stripArtist() {
	before, after, ok := strings.Cut(c.Title, " - ")
	if !ok || c.Artist == "" || after == "" || !sameName(before, c.Artist) {
		return
	}
	c.Title = strings.TrimSpace(after)
	c.fix("took the artist's name off the title")
}

// unslug turns a title made from a file name back into words: "Rosa_Parks" is "Rosa Parks",
// "sublime-sinsemilla-romeo" by Sublime is "Sinsemilla Romeo". A title joined by hyphens alone
// may be a real one ("Fu-gee-la"), so it is only taken for a slug when it is all lower case and
// starts with the artist's name, or has more hyphens than a word would.
func (c *Cleaned) unslug() {
	t := c.Title
	toWords := strings.NewReplacer("_", " ", "-", " ")
	words := strings.Fields(toWords.Replace(t))
	artist := strings.Fields(toWords.Replace(strings.ToLower(c.Artist)))
	byArtist := len(artist) > 0 && len(words) > len(artist) &&
		strings.EqualFold(strings.Join(words[:len(artist)], " "), strings.Join(artist, " "))
	hyphens := !strings.Contains(t, " ") && t == strings.ToLower(t) && (byArtist || strings.Count(t, "-") >= 3)
	if !strings.Contains(t, "_") && !hyphens {
		return
	}
	if byArtist {
		words = words[len(artist):]
	}
	t = strings.Join(words, " ")
	if t == strings.ToLower(t) {
		for i, w := range words {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			words[i] = string(r)
		}
		t = strings.Join(words, " ")
	}
	if t == "" {
		return
	}
	c.Title = t
	c.fix("turned a file name into a title")
	c.flag(FlagSlug)
}

// fillFromName fills an empty track, disc number or year from the File's name and folder.
func (c *Cleaned) fillFromName() {
	if m := stemDiscTrackRe.FindStringSubmatch(c.Stem); m != nil {
		if c.Disc == 0 && atoi(m[1]) > 0 {
			c.Disc = atoi(m[1])
			c.fix("set the disc number")
		}
		c.setTrack(atoi(m[2]))
	} else if m := stemTrackRe.FindStringSubmatch(c.Stem); m != nil {
		c.setTrack(atoi(m[1] + m[2] + m[3]))
	}
	if m := discFolderRe.FindStringSubmatch(c.Folder); m != nil && c.Disc == 0 && atoi(m[1]) > 0 {
		c.Disc = atoi(m[1])
		c.fix("set the disc number")
	}
	if m := yearFolderRe.FindStringSubmatch(c.Folder); m != nil && c.Year == 0 {
		c.Year = atoi(m[1])
		c.fix("set the year")
	}
}

func squeeze(s string) string { return spacesRe.ReplaceAllString(strings.TrimSpace(s), " ") }

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func hasLetter(s string) bool { return strings.IndexFunc(s, unicode.IsLetter) >= 0 }

// sameName compares two names the way a reader would: case and spacing aside.
func sameName(a, b string) bool { return strings.EqualFold(squeeze(a), squeeze(b)) }

func unknownArtist(a string) bool {
	switch strings.ToLower(strings.Trim(a, "[] ")) {
	case "", "unknown", "unknown artist", "artist":
		return true
	}
	return false
}

func unknownAlbum(a string) bool {
	switch strings.ToLower(strings.Trim(a, "[] ")) {
	case "", "unknown", "unknown album", "album":
		return true
	}
	return false
}
