package rip

import (
	"fmt"

	"go.senan.xyz/taglib"
)

// Tag names a Rip carries, read by the "Ripped" badge (#29). TagLib writes them as Vorbis comments
// in Opus Files and as ----:com.apple.iTunes:<name> freeform atoms in M4A Files.
const (
	TagSource    = "SOURCE"
	TagSourceURL = "SOURCEURL"
)

// WriteTags writes the Rip's source and its basic title and artist into the File, keeping any
// other tags already there.
func WriteTags(path string, info *Info) error {
	tags := map[string][]string{}
	for k, v := range map[string]string{
		taglib.Title:  info.Title,
		taglib.Artist: info.Artist,
		TagSource:     info.Source,
		TagSourceURL:  info.SourceURL,
	} {
		if v != "" {
			tags[k] = []string{v}
		}
	}
	if err := taglib.WriteTags(path, tags, 0); err != nil {
		return fmt.Errorf("writing tags to %s: %w", path, err)
	}
	return nil
}
