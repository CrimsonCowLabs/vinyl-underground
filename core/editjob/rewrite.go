package editjob

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/metadata"
)

// Rewrite is a tag edit worked out from each File's own fields as it is written, so one Rewrite
// can be given to many Files that hold different values: e.g. renaming an Artist among a Track's
// other artists, or setting an album's year in Files dated differently. It is applied in this
// order: Rename, Set (with Drop), Year. Fields are by tagedit.Field key.
type Rewrite struct {
	// Set are fields set outright, as a tags step's (TagsStep) values.
	Set tagedit.Values `json:"set,omitempty"`
	// Drop lists, for a field Set changes in a File, fields removed along with it: e.g. with a new
	// album artist, the albumartists and the MusicBrainz album artist IDs, which name the old one.
	Drop map[string][]string `json:"drop,omitempty"`
	// Year, when set, puts each date the File holds (date, originaldate) in the year: a date in
	// another year becomes just the year, one in it is kept whole. A File with neither gets the
	// year as its date.
	Year int `json:"year,omitempty"`
	// Rename, when set, renames Artists in the artist and album artist fields.
	Rename *Rename `json:"rename,omitempty"`
}

// Rename replaces the names From with To in a File's artist fields: in the multi-valued ones
// (artists, albumartists) only the values that are one of From (as model.ArtistNameKey compares
// them), and in the single ones (artist, albumartist) the name where it stands whole (see
// metadata.ReplaceName), so the File's other artists are left alone. A value that ends up twice is
// kept once, and so is its MusicBrainz ID, where the File has one per value.
type Rename struct {
	From []string `json:"from"`
	To   string   `json:"to"`
}

// renameFields are the fields Rename renames in: each display field, its multi-valued field, and
// the MusicBrainz IDs that go with the multi-valued one's values.
var renameFields = [][3]string{
	{"artist", "artists", "musicbrainz_artistid"},
	{"albumartist", "albumartists", "musicbrainz_albumartistid"},
}

// RewriteStep is the step that writes the rewrite into the File of the Track trackID.
func RewriteStep(trackID string, r Rewrite) (model.EditJobStep, error) {
	args, err := json.Marshal(r)
	if err != nil {
		return model.EditJobStep{}, err
	}
	return model.EditJobStep{Kind: model.EditRewrite, MediaFileID: trackID, Args: string(args)}, nil
}

// Apply works out the edit (the fields to change) for a File whose fields are cur.
func (r Rewrite) Apply(cur tagedit.Values) (tagedit.Values, error) {
	edit := tagedit.Values{}
	value := func(k string) []string {
		if v, ok := edit[k]; ok {
			return v
		}
		return cur[k]
	}
	if r.Rename != nil && r.Rename.To != "" {
		r.Rename.apply(cur, edit)
	}
	for k, vs := range r.Set {
		if slices.Equal(vs, value(k)) {
			continue
		}
		edit[k] = vs
		for _, d := range r.Drop[k] {
			if len(value(d)) > 0 {
				edit[d] = []string{}
			}
		}
	}
	if r.Year != 0 {
		if r.Year < 1 || r.Year > 9999 {
			return nil, fmt.Errorf("%w: %d is no year", tagedit.ErrInvalidTags, r.Year)
		}
		year := fmt.Sprintf("%04d", r.Year)
		dated := false
		for _, k := range []string{"date", "originaldate"} {
			vs := value(k)
			if len(vs) == 0 || vs[0] == "" {
				continue
			}
			dated = true
			if y, err := strconv.Atoi(firstN(vs[0], 4)); err != nil || y != r.Year {
				edit[k] = []string{year}
			}
		}
		if !dated {
			edit["date"] = []string{year}
		}
	}
	return edit, nil
}

func (rn *Rename) apply(cur, edit tagedit.Values) {
	from := map[string]bool{}
	for _, f := range rn.From {
		from[model.ArtistNameKey(f)] = true
	}
	for _, fs := range renameFields {
		display, multi, mbids := fs[0], fs[1], fs[2]
		if vs := cur[display]; len(vs) > 0 {
			renamed := slices.Clone(vs)
			for i, v := range renamed {
				for _, f := range rn.From {
					v = metadata.ReplaceName(v, f, rn.To)
				}
				renamed[i] = v
			}
			if !slices.Equal(renamed, vs) {
				edit[display] = renamed
			}
		}
		vs := cur[multi]
		ids := cur[mbids]
		positional := len(ids) == len(vs)
		var renamed, keptIDs []string
		var wasRenamed []bool
		changed := false
		for i, v := range vs {
			isRenamed := from[model.ArtistNameKey(v)]
			if isRenamed {
				v, changed = rn.To, true
			}
			at := slices.IndexFunc(renamed, func(o string) bool { return model.ArtistNameKey(o) == model.ArtistNameKey(v) })
			if at >= 0 {
				// Credited once, with the ID of the Artist that had the name already.
				changed = true
				if positional && wasRenamed[at] && !isRenamed {
					keptIDs[at], wasRenamed[at] = ids[i], false
				}
				continue
			}
			renamed, wasRenamed = append(renamed, v), append(wasRenamed, isRenamed)
			if positional {
				keptIDs = append(keptIDs, ids[i])
			}
		}
		if !changed {
			continue
		}
		edit[multi] = renamed
		if positional && len(keptIDs) != len(ids) {
			edit[mbids] = keptIDs
		}
	}
}

func firstN(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

// rewriteKind writes rewrite steps, with tagedit.Editor.RewriteInJob.
type rewriteKind struct {
	tagsKind
}

func (k rewriteKind) Run(ctx context.Context, jobID string, step *model.EditJobStep) (Outcome, error) {
	var r Rewrite
	if err := json.Unmarshal([]byte(step.Args), &r); err != nil {
		return Outcome{}, fmt.Errorf("%w: %v", tagedit.ErrInvalidTags, err)
	}
	res, err := k.editor.RewriteInJob(ctx, jobID, step.MediaFileID, r.Apply)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Changed: res.Changed, EntryID: res.Entry, Warning: res.Warning}, nil
}
