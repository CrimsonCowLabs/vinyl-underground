package corrections

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/metadata"
	"github.com/navidrome/navidrome/persistence"
)

// The Correction migration (#165, ADR-0011) writes every Correction the retired overlay applied
// on read into the Files, as one edit job: an album Correction's title, album artist and year,
// track numbers, artist renames and merges into each File they apply to, and a chosen or uploaded
// cover as its album's cover.jpg. Each Correction is deleted once all its Files are written (and
// checked, by the write path), so a File's tags are once again the only source of truth. One whose
// Files couldn't all be written stays, with the reasons, and the migration can be run again: what
// a File holds already writes nothing.
//
// An album Correction's MusicBrainz release, picked with Find on MusicBrainz before #170 wrote
// picks into the Files, is written too: its musicbrainz_albumid and musicbrainz_releasegroupid into
// the album's Files, as the overlay applied them, and its Cover Art Archive cover, which the album
// showed over every other, as its cover.jpg (a chosen cover outranked it, and wins here too).
//
// What stays app data: an image marked wrong for an album (a cover block).

// JobMigrate is the edit job of the Correction migration.
const JobMigrate model.EditKind = "migrateCorrections"

// Migration is the migration's plan, as the admin sees it before approving it.
type Migration struct {
	Items []MigrationItem `json:"items"`
	// Files is how many Files the job writes (Tracks, and album covers).
	Files int `json:"files"`
}

// MigrationItem is one Correction, what writing it changes, and how many of its Files can be
// written (Flags are those that can't, and why).
type MigrationItem struct {
	Correction model.Correction  `json:"correction"`
	Changes    []MigrationChange `json:"changes"`
	Files      int               `json:"files"`
	Flags      []FileFlag        `json:"flags"`
	// LastRun are the reasons the last migration left the Correction in place.
	LastRun []FileFlag `json:"lastRun,omitempty"`
}

// MigrationChange is one value writing a Correction changes, by field (album, albumartist,
// year, musicbrainz_albumid, musicbrainz_releasegroupid, tracknumber, discnumber, artist, cover; undoing for a Correction an undo left half done,
// which is only removed).
type MigrationChange struct {
	Field string `json:"field"`
	From  string `json:"from,omitempty"`
	To    string `json:"to"`
}

// FileFlag is a File the migration can't write (or didn't), with a code the UI translates
// (missing, changedOnDisk, unsupportedFormat, editingOff, imageMissing, ...).
type FileFlag struct {
	Path  string `json:"path"`
	Code  string `json:"code"`
	Error string `json:"error,omitempty"`
}

// codeMissing is a File the scanner found missing, or that no Track has; codeImageMissing a
// chosen cover whose image isn't in the chosen-cover store any more.
const (
	codeMissing      = "missing"
	codeImageMissing = "imageMissing"
	codeChangedSince = "changedSince"
)

// migrateArgs are the Args of the migration job: one part per Correction.
type migrateArgs struct {
	Parts []migratePart `json:"parts"`
	// Restore are the Corrections the job removed or trimmed, as they were, in that order; undoing
	// the job puts them back (newest first), so the migration can be run again.
	Restore []savedCorrection `json:"restore,omitempty"`
}

type migratePart struct {
	// Correction is the Correction as planned: the job removes it only if it is still so.
	Correction savedCorrection `json:"correction"`
	// Tracks are the Files written for it; CoverPath the cover File written for a chosen cover.
	Tracks    []string `json:"tracks,omitempty"`
	CoverPath string   `json:"coverPath,omitempty"`
	// Flags are its Files left out of the job: it stays.
	Flags []FileFlag `json:"flags,omitempty"`
	// FromID and ToID are, for an artist rename or merge, the old name's Artist and the new one's.
	FromID string `json:"fromId,omitempty"`
	ToID   string `json:"toId,omitempty"`

	// Removed says the job removed (or trimmed) the Correction; Merged that it merged the old
	// name's stars and ratings into the new name's; Skipped why it left the Correction.
	Removed bool       `json:"removed,omitempty"`
	Merged  bool       `json:"merged,omitempty"`
	Skipped []FileFlag `json:"skipped,omitempty"`
}

func (p migratePart) artist() bool {
	return p.Correction.Kind.IsArtistRename() && p.FromID != p.ToID
}

func (a migrateArgs) String() string {
	b, _ := json.Marshal(a)
	return string(b)
}

func migrateArgsOf(job *model.EditJob) (migrateArgs, error) {
	var a migrateArgs
	if err := json.Unmarshal([]byte(job.Args), &a); err != nil {
		return a, fmt.Errorf("reading the migration job's args: %w", err)
	}
	return a, nil
}

// migrates says whether the migration writes the Correction (or, half undone, removes it): every
// kind the overlay applied on read (an album Correction's MusicBrainz release too), but not a
// cover block.
func migrates(cr model.Correction) bool {
	switch {
	case cr.Undoing:
		return cr.Kind != model.CorrectionAlbumCoverBlock
	case cr.Kind == model.CorrectionAlbum:
		return cr.AlbumName != "" || cr.AlbumArtist != "" || cr.Year != 0 || cr.MbzAlbumID != "" || cr.MbzReleaseGroupID != ""
	}
	return cr.Kind.IsArtistRename() || cr.Kind == model.CorrectionTrackNumbers || cr.Kind == model.CorrectionAlbumCover
}

func (c *corrections) PlanMigration(ctx context.Context, editor *tagedit.Editor) (*Migration, error) {
	m, _, err := c.migration(ctx, editor, false)
	return m, err
}

func (c *corrections) MigrationJob(ctx context.Context, editor *tagedit.Editor) (*Plan, error) {
	_, plan, err := c.migration(ctx, editor, true)
	if err != nil {
		return nil, err
	}
	if len(plan.Steps) > 0 {
		return plan, nil
	}
	// No File to write: the Corrections that apply to nothing are removed now, as the job's Done
	// would; the others stay, flagged.
	if err := (migrateKind{ds: c.ds}).Done(ctx, &model.EditJob{Args: plan.Args}, nil); err != nil {
		return nil, err
	}
	return nil, ErrNothingToWrite
}

// fileWrite is what the migration writes into one File, and the parts it writes it for.
type fileWrite struct {
	mf       model.MediaFile
	rewrite  editjob.Rewrite
	numbered bool
}

// migration works out the plan and, with steps, the job (keeping the covers' images in the Edit
// journal's images, which are never swept: only for the job).
func (c *corrections) migration(ctx context.Context, editor *tagedit.Editor, steps bool) (*Migration, *Plan, error) {
	all, err := c.ds.Correction(ctx).GetAll()
	if err != nil {
		return nil, nil, err
	}
	renames := map[string]model.Correction{}
	for _, cr := range all {
		if cr.Kind.IsArtistRename() && !cr.Undoing {
			renames[cr.MatchKey] = cr
		}
	}
	// resolve follows renames of renamed Artists, as the overlay did (stopping at a loop).
	resolve := func(name string) string {
		var seen []string
		for {
			cr, ok := renames[model.ArtistNameKey(name)]
			if !ok || slices.Contains(seen, cr.ID) {
				return name
			}
			seen, name = append(seen, cr.ID), cr.To
		}
	}
	lastRun, err := c.lastMigration(ctx)
	if err != nil {
		return nil, nil, err
	}
	// The album Correction the overlay applied to a File: the one keyed by its folder, else the
	// newest matching it (see the overlay's MatchAlbumFolders), by folder.
	albumFolders := map[string][]string{}
	albumOwner := map[string]string{}
	for _, cr := range all {
		if cr.Kind == model.CorrectionAlbum && !cr.Undoing {
			albumOwner[cr.MatchKey] = cmp.Or(albumOwner[cr.MatchKey], cr.ID)
		}
	}
	for _, cr := range all {
		if cr.Kind != model.CorrectionAlbum || cr.Undoing {
			continue
		}
		folders, err := c.ds.Correction(ctx).AlbumFolderIDs(cr.MatchKey)
		if err != nil {
			return nil, nil, err
		}
		albumFolders[cr.ID] = folders
		for _, f := range folders {
			albumOwner[f] = cmp.Or(albumOwner[f], cr.ID)
		}
	}

	m := &Migration{Items: []MigrationItem{}}
	var args migrateArgs
	writes := map[string]*fileWrite{}
	checked := map[string]*FileFlag{}
	// check flags a File the write path would refuse before reading it.
	check := func(mf model.MediaFile) *FileFlag {
		if f, ok := checked[mf.ID]; ok {
			return f
		}
		var flag *FileFlag
		if mf.Missing {
			flag = &FileFlag{Path: mf.Path, Code: codeMissing, Error: "the File is missing"}
		} else if _, err := editor.CheckFile(ctx, mf.ID); err != nil {
			flag = &FileFlag{Path: mf.Path, Code: tagedit.Code(err), Error: err.Error()}
		}
		checked[mf.ID] = flag
		return flag
	}
	var coverSteps []model.EditJobStep
	// covers are the cover steps by the cover File they write, and the place of the folder
	// keying the choice among its album's folders: two choices for one album (keyed by two of its
	// folders) write one cover, the one the overlay showed (keyed by the earliest folder).
	type coverAt struct{ step, at int }
	covers := map[string]coverAt{}
	// addCover writes one cover File's step: the one ranked first (at).
	addCover := func(step model.EditJobStep, rel string, at int) string {
		if prev, ok := covers[rel]; !ok {
			covers[rel] = coverAt{step: len(coverSteps), at: at}
			coverSteps = append(coverSteps, step)
		} else if at < prev.at {
			coverSteps[prev.step] = step
			covers[rel] = coverAt{step: prev.step, at: at}
		}
		return rel
	}

	for _, cr := range all {
		if !migrates(cr) {
			continue
		}
		item := MigrationItem{Correction: cr, Changes: []MigrationChange{}, Flags: []FileFlag{}, LastRun: lastRun[partKey(cr)]}
		part := migratePart{Correction: save(cr)}
		// add puts the File in the part, or flags it; edit says what it writes into it.
		add := func(mf model.MediaFile, edit func(w *fileWrite)) {
			if flag := check(mf); flag != nil {
				part.Flags = append(part.Flags, *flag)
				return
			}
			w, ok := writes[mf.ID]
			if !ok {
				w = &fileWrite{mf: mf}
				writes[mf.ID] = w
			}
			edit(w)
			part.Tracks = append(part.Tracks, mf.ID)
		}

		switch {
		case cr.Undoing:
			item.Changes = append(item.Changes, MigrationChange{Field: "undoing", From: cr.From, To: cr.To})

		case cr.Kind == model.CorrectionAlbum:
			albumArtist := resolve(cr.AlbumArtist)
			if cr.AlbumName != "" {
				item.Changes = append(item.Changes, MigrationChange{Field: "album", To: cr.AlbumName})
			}
			if cr.AlbumArtist != "" {
				item.Changes = append(item.Changes, MigrationChange{Field: "albumartist", To: albumArtist})
			}
			if cr.Year != 0 {
				item.Changes = append(item.Changes, MigrationChange{Field: "year", To: strconv.Itoa(cr.Year)})
			}
			release := tagedit.Values{}
			if cr.MbzAlbumID != "" {
				release["musicbrainz_albumid"] = []string{cr.MbzAlbumID}
			}
			if cr.MbzReleaseGroupID != "" {
				release["musicbrainz_releasegroupid"] = []string{cr.MbzReleaseGroupID}
			}
			for _, k := range []string{"musicbrainz_albumid", "musicbrainz_releasegroupid"} {
				if v, ok := release[k]; ok {
					item.Changes = append(item.Changes, MigrationChange{Field: k, To: v[0]})
				}
			}
			mfs, err := c.migrationFiles(ctx, squirrel.Eq{"media_file.folder_id": albumFolders[cr.ID]})
			if err != nil {
				return nil, nil, err
			}
			// The album's ID comes from its musicbrainz_albumid: a release written into only some
			// of its Files would split it, so it is written only if all of them can be.
			if len(release) > 0 {
				for _, mf := range mfs {
					if albumOwner[mf.FolderID] == cr.ID && check(mf) != nil {
						release = nil
						break
					}
				}
			}
			for _, mf := range mfs {
				// Another album Correction applied to this File's folder.
				if albumOwner[mf.FolderID] != cr.ID {
					continue
				}
				add(mf, func(w *fileWrite) {
					r := &w.rewrite
					if r.Set == nil {
						r.Set = tagedit.Values{}
					}
					if cr.AlbumName != "" {
						r.Set["album"] = []string{cr.AlbumName}
					}
					if cr.AlbumArtist != "" {
						r.Set["albumartist"] = []string{albumArtist}
						r.Drop = map[string][]string{"albumartist": newAlbumArtistDrops}
					}
					if cr.Year != 0 {
						r.Year = cr.Year
					}
					maps.Copy(r.Set, release)
				})
			}
			// The release's cover, which the album showed over every other: a chosen cover
			// outranked it, so it is written only where the album has none. A cover that can't
			// be written (its image gone, ...) leaves the Correction's IDs to be written all the
			// same: nothing shows it any more.
			if cr.ImageHash != "" {
				step, rel, at, flag, err := c.migrationCover(ctx, editor, cr, steps)
				if err != nil {
					return nil, nil, err
				}
				if flag == nil && rel != "" {
					item.Changes = append(item.Changes, MigrationChange{Field: "cover", From: pickCoverLabel, To: rel})
					part.CoverPath = addCover(step, rel, pickRank+at)
				}
			}

		case cr.Kind == model.CorrectionTrackNumbers:
			for _, f := range cr.TrackNumbers {
				item.Changes = append(item.Changes, numberChanges(f)...)
				mfs, err := c.migrationFiles(ctx, squirrel.Eq{"media_file.library_id": f.LibraryID, "media_file.path": f.Path})
				if err != nil {
					return nil, nil, err
				}
				if len(mfs) == 0 {
					part.Flags = append(part.Flags, FileFlag{Path: f.Path, Code: codeMissing, Error: "no Track has the File"})
					continue
				}
				add(mfs[0], func(w *fileWrite) {
					// Should two Corrections number one File, the newest (listed first) wins, as
					// in the overlay.
					if w.numbered {
						return
					}
					w.numbered = true
					if w.rewrite.Set == nil {
						w.rewrite.Set = tagedit.Values{}
					}
					if f.Track > 0 && f.Track != f.FromTrack {
						w.rewrite.Set["tracknumber"] = []string{strconv.Itoa(f.Track)}
					}
					if f.Disc > 0 && f.Disc != f.FromDisc {
						w.rewrite.Set["discnumber"] = []string{strconv.Itoa(f.Disc)}
					}
				})
			}

		case cr.Kind.IsArtistRename():
			to := resolve(cr.To)
			item.Changes = append(item.Changes, MigrationChange{Field: "artist", From: cr.From, To: to})
			part.FromID, part.ToID = metadata.ArtistID(cr.From), metadata.ArtistID(to)
			// The Tracks the overlay renamed (it noted the Corrections it applied), and those a
			// scan since the upgrade read as tagged.
			mfs, err := c.migrationFiles(ctx, squirrel.Or{
				squirrel.Expr("exists (select 1 from json_each(nullif(media_file.correction_ids, '')) je where je.value = ?)", cr.ID),
				persistence.ParticipantIDFilter("media_file", part.FromID, model.RoleArtist, model.RoleAlbumArtist),
			})
			if err != nil {
				return nil, nil, err
			}
			for _, mf := range mfs {
				add(mf, func(w *fileWrite) {
					w.rewrite.Renames = append(w.rewrite.Renames, editjob.Rename{From: []string{cr.From}, To: to})
				})
			}

		case cr.Kind == model.CorrectionAlbumCover:
			step, rel, at, flag, err := c.migrationCover(ctx, editor, cr, steps)
			if err != nil {
				return nil, nil, err
			}
			item.Changes = append(item.Changes, MigrationChange{Field: "cover", From: cr.To, To: rel})
			switch {
			case flag != nil:
				part.Flags = append(part.Flags, *flag)
			case rel != "":
				part.CoverPath = addCover(step, rel, at)
			}
		}
		item.Files = len(part.Tracks)
		if part.CoverPath != "" {
			item.Files++
		}
		item.Flags = append(item.Flags, part.Flags...)
		m.Items = append(m.Items, item)
		args.Parts = append(args.Parts, part)
	}

	// The covers go first: an album's ID comes from its tags, which the rest may change.
	plan := &Plan{Kind: JobMigrate}
	plan.Steps = append(plan.Steps, coverSteps...)
	ids := make([]string, 0, len(writes))
	for id := range writes {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int { return cmp.Compare(writes[a].mf.Path, writes[b].mf.Path) })
	for _, id := range ids {
		w := writes[id]
		step, err := editjob.RewriteStep(id, w.rewrite)
		if err != nil {
			return nil, nil, err
		}
		plan.Steps = append(plan.Steps, step)
	}
	m.Files = len(plan.Steps)
	plan.Args = args.String()
	return m, plan, nil
}

// pickRank ranks a picked release's cover after every chosen cover of its album; pickCoverLabel
// names it.
const (
	pickRank       = 1 << 20
	pickCoverLabel = "Cover Art Archive (MusicBrainz release)"
)

// numberChanges are what writing a track numbers fix changes: "3 Two" → 2.
func numberChanges(f model.TrackNumberFix) []MigrationChange {
	var res []MigrationChange
	if f.Disc > 0 && f.Disc != f.FromDisc {
		res = append(res, MigrationChange{Field: "discnumber", From: fmt.Sprintf("%d %s", f.FromDisc, f.Title), To: strconv.Itoa(f.Disc)})
	}
	if f.Track > 0 && f.Track != f.FromTrack {
		res = append(res, MigrationChange{Field: "tracknumber", From: fmt.Sprintf("%d %s", f.FromTrack, f.Title), To: strconv.Itoa(f.Track)})
	}
	return res
}

// migrationFiles returns the Tracks matching the filter, missing ones too (they are flagged).
func (c *corrections) migrationFiles(ctx context.Context, filter squirrel.Sqlizer) (model.MediaFiles, error) {
	return c.ds.MediaFile(ctx).GetAll(model.QueryOptions{Sort: "path", Filters: filter})
}

// migrationCover works out the cover write of a chosen cover Correction: where the cover File goes
// (rel; "" when no album is in its folder any more: it applies to nothing), the place of its
// folder among the album's, or why it can't be written, and, with steps, the step.
func (c *corrections) migrationCover(ctx context.Context, editor *tagedit.Editor, cr model.Correction, steps bool) (model.EditJobStep, string, int, *FileFlag, error) {
	albums, err := c.ds.Album(ctx).GetAll(model.QueryOptions{Sort: "id",
		Filters: squirrel.Expr("exists (select 1 from json_each(album.folder_ids) af where af.value = ?)", cr.MatchKey)})
	if err != nil || len(albums) == 0 {
		return model.EditJobStep{}, "", 0, nil, err
	}
	al := albums[0]
	at := slices.Index(al.FolderIDs, cr.MatchKey)
	flagged := func(path string, code string, err error) (model.EditJobStep, string, int, *FileFlag, error) {
		return model.EditJobStep{}, path, at, &FileFlag{Path: path, Code: code, Error: err.Error()}, nil
	}
	rel, err := editor.CheckCover(ctx, al.ID)
	if err != nil {
		return flagged(al.Name, tagedit.Code(err), err)
	}
	image, err := readChosen(cr)
	if err != nil {
		return flagged(rel, codeImageMissing, err)
	}
	if !steps {
		if _, err := tagedit.CoverJPEG(image); err != nil {
			return flagged(rel, tagedit.Code(err), err)
		}
		return model.EditJobStep{}, rel, at, nil, nil
	}
	step, err := editjob.CoverStep(editor, al.ID, image)
	if err != nil {
		return flagged(rel, tagedit.Code(err), err)
	}
	step.Path = rel
	return step, rel, at, nil, nil
}

func readChosen(cr model.Correction) ([]byte, error) {
	f, err := artwork.ChosenCoverStore().Open(cr.ImageHash, cr.ImageMime)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// partKey names a Correction across its removal and putting back (which gives it a new ID).
func partKey(cr model.Correction) string {
	return string(cr.Kind) + "|" + cr.MatchKey
}

// lastMigration returns, by partKey, why the latest migration job left each Correction it left.
func (c *corrections) lastMigration(ctx context.Context) (map[string][]FileFlag, error) {
	jobs, err := c.ds.EditJob(ctx).GetAll(50)
	if err != nil {
		return nil, err
	}
	res := map[string][]FileFlag{}
	for _, job := range jobs {
		if job.Kind != JobMigrate || job.State != model.EditJobDone {
			continue
		}
		full, err := c.ds.EditJob(ctx).Get(job.ID)
		if err != nil {
			return nil, err
		}
		a, err := migrateArgsOf(full)
		if err != nil {
			return nil, err
		}
		for _, p := range a.Parts {
			if len(p.Skipped) > 0 {
				res[partKey(p.Correction.correction())] = p.Skipped
			}
		}
		break
	}
	return res, nil
}

// migrateKind keeps the Artists' stars and ratings as the migration job starts, and once its
// Files are written removes each Correction whose Files all are; its undo puts them back.
type migrateKind struct {
	ds model.DataStore
}

// Start keeps what the job's undo needs of each renamed Artist's stars and ratings, before any
// File is written (the scans after each File purge an Artist no File names any more, and its
// annotations with it). The overlay had moved them already, and the Correction kept them: an
// artist merge what it merged (adopted, as merged), a plain rename the new name's, as the app
// started (see KeepRenamedStars; adopted, to merge into the new name again, should a scan have
// purged it). With none kept, a plain rename the overlay applied keeps the new name's now, once
// per new name (a chain A→B→C would give A and B both C's on undo). An Artist whose Files still
// name it (the job undone, and run again) keeps its own, as an artist rename job's do.
func (k migrateKind) Start(ctx context.Context, job *model.EditJob) error {
	a, err := migrateArgsOf(job)
	if err != nil {
		return err
	}
	return k.ds.WithTx(func(tx model.DataStore) error {
		jobs := tx.EditJob(ctx)
		keptTo := map[string]bool{}
		for i, p := range a.Parts {
			if !p.artist() || p.Removed {
				continue
			}
			part := strconv.Itoa(i)
			merge := p.Correction.Kind == model.CorrectionArtistMerge
			cur, err := tx.Correction(ctx).GetByMatchKey(p.Correction.Kind, p.Correction.MatchKey)
			if err != nil && !errors.Is(err, model.ErrNotFound) {
				return err
			}
			if cur != nil {
				n, err := jobs.AdoptCorrectionAnnotations(job.ID, part, cur.ID, merge)
				if err != nil {
					return err
				}
				if n > 0 {
					continue
				}
			}
			if !merge {
				overlaid, err := k.renamedAway(ctx, tx, p)
				if err != nil {
					return err
				}
				if overlaid {
					if !keptTo[p.ToID] {
						keptTo[p.ToID] = true
						if err := jobs.KeepArtistAnnotationsIn(job.ID, part, p.ToID); err != nil {
							return err
						}
					}
					continue
				}
			}
			if err := jobs.KeepArtistAnnotationsIn(job.ID, part, p.FromID); err != nil {
				return err
			}
		}
		return nil
	})
}

// KeepRenamedStars keeps, with each artist rename Correction not yet migrated (#165), its new
// name's stars and ratings (CorrectionRepository.KeepArtistAnnotations), which the overlay moved
// there: the scanner reads the old name now, and a scan before the migration would purge the new
// one, and them with it. It runs as the app starts, before any scan; what was kept stays kept.
func KeepRenamedStars(ctx context.Context, ds model.DataStore) error {
	repo := ds.Correction(ctx)
	all, err := repo.GetAll()
	if err != nil {
		return err
	}
	for _, cr := range all {
		if cr.Kind != model.CorrectionArtistRename || cr.Undoing {
			continue
		}
		if err := repo.KeepArtistAnnotations(cr.ID, metadata.ArtistID(cr.To)); err != nil {
			return err
		}
	}
	return nil
}

// renamedAway says whether the overlay renamed the old name away: the new name is an Artist, and
// the old one isn't.
func (k migrateKind) renamedAway(ctx context.Context, tx model.DataStore, p migratePart) (bool, error) {
	exists := func(id string) (bool, error) {
		n, err := tx.Artist(ctx).CountAll(model.QueryOptions{Filters: squirrel.Eq{"artist.id": id}})
		return n > 0, err
	}
	to, err := exists(p.ToID)
	if err != nil || !to {
		return false, err
	}
	from, err := exists(p.FromID)
	return !from, err
}

func (k migrateKind) Done(ctx context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	a, err := migrateArgsOf(job)
	if err != nil {
		return err
	}
	byTrack, byCover := map[string]model.EditJobStep{}, map[string]model.EditJobStep{}
	for _, s := range steps {
		if s.MediaFileID == "" {
			byCover[s.Path] = s
		} else {
			byTrack[s.MediaFileID] = s
		}
	}
	// A chosen cover's removal trims the image off its album's picked release, which the album's
	// own Correction must still find as planned: covers go last.
	order := make([]int, 0, len(a.Parts))
	for _, cover := range []bool{false, true} {
		for i, p := range a.Parts {
			if (p.Correction.Kind == model.CorrectionAlbumCover) == cover {
				order = append(order, i)
			}
		}
	}
	return k.ds.WithTx(func(tx model.DataStore) error {
		repo := tx.Correction(ctx)
		for _, i := range order {
			p := &a.Parts[i]
			if p.Removed {
				continue
			}
			reasons := slices.Clone(p.Flags)
			written := false
			outcome := func(s model.EditJobStep, ok bool) {
				switch {
				case !ok:
					reasons = append(reasons, FileFlag{Code: tagedit.CodeInternal, Error: "the job has no step for the File"})
				case s.State == model.EditStepSkipped:
					reasons = append(reasons, FileFlag{Path: s.Path, Code: s.Code, Error: s.Error})
				case s.State == model.EditStepWritten || s.State == model.EditStepUnchanged:
					written = true
				}
			}
			for _, t := range p.Tracks {
				s, ok := byTrack[t]
				outcome(s, ok)
			}
			if p.CoverPath != "" {
				s, ok := byCover[p.CoverPath]
				outcome(s, ok)
			}
			if p.artist() && written && !p.Merged {
				if err := tx.EditJob(ctx).MergeArtistAnnotationsIn(job.ID, strconv.Itoa(i), p.FromID, p.ToID); err != nil {
					return err
				}
				p.Merged = true
			}
			if len(reasons) > 0 {
				p.Skipped = reasons
				continue
			}
			p.Skipped = nil
			saved, removed, err := removeMigrated(repo, p.Correction)
			if err != nil {
				return err
			}
			a.Restore = append(a.Restore, saved...)
			if !removed {
				p.Skipped = []FileFlag{{Code: codeChangedSince, Error: "the Correction changed after the migration was planned"}}
				continue
			}
			p.Removed = true
		}
		log.Info(ctx, "Corrections: migrated Corrections into the Files", "job", job.ID, "corrections", len(a.Parts))
		job.Args = a.String()
		if job.ID == "" {
			return nil
		}
		return tx.EditJob(ctx).Put(job)
	})
}

// removeMigrated removes the Correction planned (if it is still as planned). A chosen cover
// outranked a picked release's cover, which a later run would write over it: an album Correction
// left with a release (its Files not all written) keeps no cover then. It returns the
// Corrections it changed, as they were, and whether the planned one is gone (false: it changed
// since it was planned, and is another Correction now, which stays).
func removeMigrated(repo model.CorrectionRepository, planned savedCorrection) ([]savedCorrection, bool, error) {
	cur, err := repo.GetByMatchKey(planned.Kind, planned.MatchKey)
	switch {
	case errors.Is(err, model.ErrNotFound):
		// A cover write removes the chosen cover itself, once written.
		cur = nil
	case err != nil:
		return nil, false, err
	case !reflect.DeepEqual(save(*cur), planned):
		return nil, false, nil
	}
	var saved []savedCorrection
	if cur != nil {
		saved = append(saved, save(*cur))
		if err := repo.Delete(cur.ID); err != nil {
			return nil, false, err
		}
	} else if planned.Kind == model.CorrectionAlbumCover {
		saved = append(saved, planned)
	}
	if planned.Kind != model.CorrectionAlbumCover {
		return saved, true, nil
	}
	folders, err := repo.AlbumFolderIDs(planned.MatchKey)
	if err != nil {
		return nil, false, err
	}
	picks, err := repo.FindByMatchKeys(folders, model.CorrectionAlbum)
	if err != nil {
		return nil, false, err
	}
	for _, pick := range picks {
		if pick.ImageHash == "" || pick.Undoing {
			continue
		}
		trimmed := pick
		trimmed.ImageHash, trimmed.ImageMime = "", ""
		if err := replaceCorrection(repo, pick, trimmed, true); err != nil {
			return nil, false, err
		}
		saved = append(saved, save(pick))
	}
	return saved, true, nil
}

// Undone puts back the Corrections the job removed, so the migration lists them again, and gives
// each renamed Artist back the stars and ratings the job merged into its new name.
func (k migrateKind) Undone(ctx context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	a, err := migrateArgsOf(job)
	if err != nil || len(written(steps)) == 0 {
		return err
	}
	err = k.ds.WithTx(func(tx model.DataStore) error {
		for i := range a.Parts {
			p := &a.Parts[i]
			if p.Merged {
				if err := tx.EditJob(ctx).SplitArtistAnnotationsIn(job.ID, strconv.Itoa(i), p.FromID, p.ToID,
					p.Correction.Kind == model.CorrectionArtistRename); err != nil {
					return err
				}
			}
			p.Merged, p.Removed = false, false
		}
		saved := slices.Clone(a.Restore)
		slices.Reverse(saved)
		if err := restore(ctx, tx, saved); err != nil {
			return err
		}
		a.Restore = nil
		return nil
	})
	if err != nil {
		return err
	}
	log.Info(ctx, "Corrections: put back the Corrections an undone migration removed", "job", job.ID)
	job.Args = a.String()
	return nil
}
