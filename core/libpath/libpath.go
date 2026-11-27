// Package libpath names the Library's folders and Files the one way the app does (ADR-0008,
// ADR-0011): `<Album Artist>/<Album> (<Year>)/<NN> <Title>.<ext>`, with `<D>-<NN>` on a
// multi-disc album, each name made safe for the NAS (SMB). Promote and Organize share it, so a
// File lands at the same path whichever of them places it.
package libpath

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// MaxNameBytes caps each folder or File name, before the " (<Year>)", "<NN> ", " (2)" and
// extension that may be added. SMB allows 255 bytes per name; this leaves room for those and for
// the hidden temp name a File is copied under.
const MaxNameBytes = 120

// SafeName makes s a safe folder or File name, or fallback if nothing is left: no
// `\ / : * ? " < > |` or control characters, no leading dot (the scanner would skip it as
// hidden), no trailing dots or spaces, no reserved device name, at most MaxNameBytes, and
// composed (NFC).
func SafeName(s, fallback string) string {
	s = norm.NFC.String(sanitize(s))
	if strings.HasPrefix(s, ".") && !strings.HasPrefix(s, "..") {
		s = s[1:]
	}
	s = strings.TrimSpace(s)
	if len(s) > MaxNameBytes {
		s = strings.ToValidUTF8(s[:MaxNameBytes], "")
	}
	s = strings.TrimRight(s, " .")
	if s == "" {
		return fallback
	}
	if reservedName(s) {
		s += "_"
	}
	return s
}

func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r):
			return -1
		case strings.ContainsRune(`/\:*?"<>|`, r):
			return '_'
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// reservedName reports whether s is a device name Windows (and so some SMB servers) refuses as
// a File or folder name.
func reservedName(s string) bool {
	switch u := strings.ToUpper(s); u {
	case "CON", "PRN", "AUX", "NUL":
		return true
	default:
		return len(u) == 4 && (strings.HasPrefix(u, "COM") || strings.HasPrefix(u, "LPT")) && u[3] >= '1' && u[3] <= '9'
	}
}

// AlbumFolder is the name of an album's folder: `<Album> (<Year>)`, or `<Album>` with no year.
// album must be a SafeName already.
func AlbumFolder(album string, year int) string {
	if year > 0 {
		return fmt.Sprintf("%s (%d)", album, year)
	}
	return album
}

// TrackFile is the name of a Track's File: `<NN> <Title><ext>`, or `<D>-<NN> <Title><ext>` on a
// multi-disc album, with no number when track is 0. title must be a SafeName already; ext is
// lowered.
func TrackFile(title string, disc, track int, multiDisc bool, ext string) string {
	ext = strings.ToLower(ext)
	switch {
	case track > 0 && multiDisc:
		return fmt.Sprintf("%d-%02d %s%s", disc, track, title, ext)
	case track > 0:
		return fmt.Sprintf("%02d %s%s", track, title, ext)
	}
	return title + ext
}

// Fold is the key two names share when the case-insensitive NAS takes them for one: compared
// ignoring letter case, once both are composed (NFC).
func Fold(name string) string {
	return cases.Fold().String(norm.NFC.String(name))
}

// SameName reports whether the NAS takes a and b for the same name (see Fold).
func SameName(a, b string) bool { return Fold(a) == Fold(b) }
