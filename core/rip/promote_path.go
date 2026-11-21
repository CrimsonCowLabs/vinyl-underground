package rip

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Fallbacks for a promoted File whose tags are missing.
const (
	unknownArtist = "Unknown Artist"
	unknownAlbum  = "Unknown Album"
	untitled      = "Untitled"
)

// maxNameBytes caps each folder or File name Promote makes, before the " (<Year>)", "<NN> ",
// " (2)" and extension it may add. SMB allows 255 bytes per name; this leaves room for those and
// for the hidden temp name a File is copied under.
const maxNameBytes = 120

// promoteNames are the three names of a promoted File's place in the promote folder:
// <Artist>/<Album>/<File>.
type promoteNames struct {
	Artist, Album, File string
}

// promoteLayout names a promoted File's place, `<Album Artist>/<Album> (<Year>)/<NN> <Title>.<ext>`,
// from its tags (ADR-0008). src is the File's current name, for its extension and for a Title
// fallback. Each name is safe for SMB: no `\ / : * ? " < > |` or control characters, no leading
// dot (the scanner would skip it as hidden), no trailing dots or spaces, no reserved device
// name, at most maxNameBytes, and composed (NFC).
func promoteLayout(tags FileTags, src string) promoteNames {
	ext := strings.ToLower(filepath.Ext(src))
	artist := safeName(cmp.Or(strings.TrimSpace(tags.AlbumArtist), strings.TrimSpace(tags.Artist)), unknownArtist)
	album := safeName(tags.Album, unknownAlbum)
	if y := tagYear(tags.Year); y > 0 {
		album = fmt.Sprintf("%s (%d)", album, y)
	}
	title := safeName(tags.Title, "")
	if title == "" {
		title = safeName(strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)), untitled)
	}
	if n := tagNumber(tags.TrackNumber); n > 0 {
		title = fmt.Sprintf("%02d %s", n, title)
	}
	return promoteNames{Artist: artist, Album: album, File: title + ext}
}

// safeName makes s a safe folder or File name (see promoteLayout), or fallback if nothing is left.
func safeName(s, fallback string) string {
	s = norm.NFC.String(sanitize(s))
	if strings.HasPrefix(s, ".") && !strings.HasPrefix(s, "..") {
		s = s[1:]
	}
	s = strings.TrimSpace(s)
	if len(s) > maxNameBytes {
		s = strings.ToValidUTF8(s[:maxNameBytes], "")
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

// ErrOutsidePromoteFolder is returned when a folder Promote would write into leads out of the
// promote folder (a symlink).
var ErrOutsidePromoteFolder = errors.New("the path is not inside the promote folder")

// promoteDest finds or makes the File's two folders under root, and returns a free path for
// it there, which it doesn't create. The NAS is case-insensitive, so a folder whose name differs
// only by letter case (once both are composed, NFC) is reused rather than made beside it, and a
// File name is free only if no name in its folder matches it that way: " (2)", " (3)"... is
// added until one is.
func promoteDest(fsys promoteFS, root string, n promoteNames) (string, error) {
	realRoot, err := fsys.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("the promote folder can't be reached: %w", err)
	}
	dir := root
	for _, name := range []string{n.Artist, n.Album} {
		if dir, err = promoteFolder(fsys, dir, name); err != nil {
			return "", err
		}
		real, err := fsys.EvalSymlinks(dir)
		if err != nil {
			return "", err
		}
		if real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
			return "", fmt.Errorf("%w: %q", ErrOutsidePromoteFolder, dir)
		}
	}
	return freeName(fsys, dir, n.File)
}

// promoteFolder returns the folder called name (in any letter case) in parent, making it if
// there is none.
func promoteFolder(fsys promoteFS, parent, name string) (string, error) {
	for range 2 {
		existing, err := foldMatch(fsys, parent, name)
		if err != nil {
			return "", err
		}
		if existing != "" {
			path := filepath.Join(parent, existing)
			info, err := fsys.Stat(path)
			if err != nil {
				return "", err
			}
			if !info.IsDir() {
				return "", fmt.Errorf("%q is in the way: it is not a folder", path)
			}
			return path, nil
		}
		err = fsys.Mkdir(filepath.Join(parent, name))
		if err == nil {
			return filepath.Join(parent, name), nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("making the folder %q: %w", name, err)
		}
		// Made meanwhile, or there under a name the listing missed: look again.
	}
	return "", fmt.Errorf("making the folder %q: it keeps appearing and disappearing", name)
}

// foldMatch returns the name in dir that is name, ignoring letter case and Unicode composition,
// or "".
func foldMatch(fsys promoteFS, dir, name string) (string, error) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return "", err
	}
	want := norm.NFC.String(name)
	for _, e := range entries {
		if strings.EqualFold(norm.NFC.String(e.Name()), want) {
			return e.Name(), nil
		}
	}
	return "", nil
}

// freeName returns a path in dir for a File called name, or "name (2)", "name (3)"..., that no
// name in dir matches (see foldMatch).
func freeName(fsys promoteFS, dir, name string) (string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		candidate := name
		if i > 1 {
			candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		taken, err := foldMatch(fsys, dir, candidate)
		if err != nil {
			return "", err
		}
		if taken == "" {
			return filepath.Join(dir, candidate), nil
		}
	}
	return "", fmt.Errorf("too many Files named %q in %q", name, dir)
}

// tagYear is the year of a date tag ("2004" or "2004-05-01"), or 0.
func tagYear(s string) int {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return 0
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil || y <= 0 {
		return 0
	}
	return y
}

// tagNumber is a track number tag's number ("3" or "3/12"), or 0.
func tagNumber(s string) int {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "/")
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}
