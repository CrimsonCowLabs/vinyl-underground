// Package match finds the Tracks the Library (and the Inbox) already seems to have for a video
// about to be ripped, from the video's artist and title. It only warns: a likely match may be
// another recording, and it is never a Duplicate (see GLOSSARY.md), which is about Files.
//
// The normaliser here is deliberately small: it strips the usual YouTube noise so a video's
// title can be compared with a Track's. Proper title cleanup for tag proposals is #34's.
package match

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/deluan/sanitize"
)

// Guess is one reading of a video's artist and title, with the YouTube noise taken out. The
// strings are still readable text, for a full-text search; compare them with Key.
type Guess struct {
	Artist string
	Title  string
}

var (
	// bracketed is a "(…)" or "[…]" group, which noiseWords decides whether to drop.
	bracketed = regexp.MustCompile(`\s*[(\[]([^()\[\]]*)[)\]]`)
	// featuring is a featuring credit and everything after it.
	featuring = regexp.MustCompile(`(?i)(\s|[(\[])(feat\.?|ft\.?|featuring)\s.*$`)
	// dash splits "Artist - Title". A hyphen inside a word (JAY-Z) is no split.
	dash = regexp.MustCompile(`\s+[-–—]\s+`)
	// channelSuffix is what a channel adds to its artist's name.
	channelSuffix = regexp.MustCompile(`(?i)(\s*-\s*topic|\s*vevo|\s+official)$`)
)

// noiseWords are the words of a bracketed group that says nothing about the recording, as in
// "(Official Music Video)", "[HD]" or "(Lyrics)". "Live", "Remastered", "Acoustic" and the like
// are kept: they make another recording.
var noiseWords = map[string]bool{
	"official": true, "music": true, "video": true, "audio": true, "lyric": true, "lyrics": true,
	"visualizer": true, "visualiser": true, "hd": true, "hq": true, "4k": true, "1080p": true,
	"720p": true, "mv": true, "m": true, "v": true, "explicit": true, "clip": true, "officiel": true,
}

// Guesses reads a video's artist and title, most likely reading first. A title of the form
// "Artist - Title" is read that way first, and then, in case the dash is part of the title (as
// in "Creep - Acoustic"), whole, with the video's artist. artist may be the uploader or channel
// when the site gives no artist. There is no guess without a title.
func Guesses(artist, title string) []Guess {
	artist = cleanArtist(artist)
	title = cleanTitle(title)
	var res []Guess
	add := func(a, t string) {
		a, t = strings.TrimSpace(a), strings.TrimSpace(t)
		if Key(t) != "" {
			res = append(res, Guess{Artist: a, Title: t})
		}
	}
	if parts := dash.Split(title, 2); len(parts) == 2 && Key(parts[0]) != "" {
		add(cleanArtist(parts[0]), parts[1])
		if Key(parts[0]) == Key(artist) || artist == "" {
			return res
		}
	}
	add(artist, title)
	return res
}

func cleanArtist(s string) string {
	s = featuring.ReplaceAllString(strings.TrimSpace(s), "")
	return strings.TrimSpace(channelSuffix.ReplaceAllString(s, ""))
}

func cleanTitle(s string) string {
	if before, _, ok := strings.Cut(s, " | "); ok {
		s = before
	}
	s = bracketed.ReplaceAllStringFunc(s, func(group string) string {
		inner := bracketed.FindStringSubmatch(group)[1]
		if isNoise(inner) || featuring.MatchString(" "+inner) {
			return ""
		}
		return group
	})
	s = featuring.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func isNoise(s string) bool {
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for _, w := range words {
		if !noiseWords[w] {
			return false
		}
	}
	return len(words) > 0
}

// Key is s for comparing: lower case, without accents, featuring credits or punctuation, "&"
// read as "and", and single spaces between words.
func Key(s string) string {
	s = featuring.ReplaceAllString(s, "")
	s = strings.ToLower(sanitize.Accents(s))
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.NewReplacer("'", "", "’", "").Replace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
