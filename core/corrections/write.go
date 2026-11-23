package corrections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/metadata"
	"github.com/navidrome/navidrome/persistence"
)

// The album Edit dialog, Track order and artist rename and merge write their fixes into the
// Files (#163, ADR-0011), as one edit job each, instead of saving a Correction. Each is a job
// kind of its own (see JobKinds), which also removes what existing Corrections override of what
// the job wrote once its Files are written, so a stale override doesn't hide the Files' new
// values, and puts them back if the job is undone.
const (
	// JobAlbumEdit writes an album's title, album artist and year (and its Tracks' numbers, if the
	// dialog changed them too) into every File of the album.
	JobAlbumEdit model.EditKind = "albumEdit"
	// JobTrackNumbers writes the disc and track numbers of an album's Tracks into their Files.
	JobTrackNumbers model.EditKind = "trackNumbers"
	// JobArtistRename renames an Artist in every File that names it as Track artist or album
	// artist; its stars, ratings and play counts go with it to the new name.
	JobArtistRename model.EditKind = "artistRename"
	// JobArtistMerge is JobArtistRename to the name of another Artist, merging the two: their
	// stars, ratings and play counts are joined, and undoing the job splits them again.
	JobArtistMerge model.EditKind = "artistMerge"
)

// ErrNothingToWrite is a save whose values every File already holds.
var ErrNothingToWrite = fmt.Errorf("%w: nothing changed", model.ErrValidation)

// Plan is the job a save writes: queue its Steps as a job of Kind with Args (see
// editjob.Queue.EnqueueWith).
type Plan struct {
	Kind  model.EditKind
	Args  string
	Steps []model.EditJobStep
	// TrackID is a Track of the album an album job writes: its album, once the job is done, is
	// the album with its new ID (album IDs come from its tags; Track IDs stay).
	TrackID string
	// ArtistID is the ID of the Artist an artist job renames, once it is done.
	ArtistID string
	// PickCover is what the Cover Art Archive answered for a picked release's cover, when the
	// job was asked to write it (JobRelease).
	PickCover PickCover
}

// writeArgs are the Args of a job of the kinds above.
type writeArgs struct {
	// Folders are the album's folders, which key its Corrections.
	Folders []string `json:"folders,omitempty"`
	// Name, AlbumArtist and Year say which of the album's values the job wrote into its Files.
	Name        bool `json:"name,omitempty"`
	AlbumArtist bool `json:"albumArtist,omitempty"`
	Year        bool `json:"year,omitempty"`
	// Numbered are the Files the job wrote disc and track numbers into.
	Numbered []fileRef `json:"numbered,omitempty"`
	// Release says the job wrote a picked MusicBrainz release's IDs into the album's Files
	// (JobRelease), and Rollback is the job it queued to put them back when it couldn't write
	// them all.
	Release  bool   `json:"release,omitempty"`
	Rollback string `json:"rollback,omitempty"`

	// FromID is the Artist an artist job renames, Names the names it goes by in the Files, To its
	// new name and ToID the new name's Artist.
	FromID string   `json:"fromId,omitempty"`
	ToID   string   `json:"toId,omitempty"`
	Names  []string `json:"names,omitempty"`
	To     string   `json:"to,omitempty"`
	Merge  bool     `json:"merge,omitempty"`

	// Restore are the Corrections as they were before the job removed (or trimmed) them, which
	// undoing it puts back.
	Restore []savedCorrection `json:"restore,omitempty"`
}

type fileRef struct {
	TrackID   string `json:"trackId"`
	LibraryID int    `json:"libraryId"`
	Path      string `json:"path"`
}

// savedCorrection is a Correction with all it holds (model.Correction leaves some out of JSON).
type savedCorrection struct {
	Kind              model.CorrectionKind   `json:"kind"`
	MatchKey          string                 `json:"matchKey"`
	From              string                 `json:"from"`
	To                string                 `json:"to"`
	ImageHash         string                 `json:"imageHash,omitempty"`
	ImageMime         string                 `json:"imageMime,omitempty"`
	AlbumName         string                 `json:"albumName,omitempty"`
	AlbumArtist       string                 `json:"albumArtist,omitempty"`
	Year              int                    `json:"year,omitempty"`
	TrackNumbers      model.TrackNumberFixes `json:"trackNumbers,omitempty"`
	MbzAlbumID        string                 `json:"mbzAlbumId,omitempty"`
	MbzReleaseGroupID string                 `json:"mbzReleaseGroupId,omitempty"`
}

func save(c model.Correction) savedCorrection {
	return savedCorrection{Kind: c.Kind, MatchKey: c.MatchKey, From: c.From, To: c.To, ImageHash: c.ImageHash,
		ImageMime: c.ImageMime, AlbumName: c.AlbumName, AlbumArtist: c.AlbumArtist, Year: c.Year,
		TrackNumbers: c.TrackNumbers, MbzAlbumID: c.MbzAlbumID, MbzReleaseGroupID: c.MbzReleaseGroupID}
}

func (s savedCorrection) correction() model.Correction {
	return model.Correction{Kind: s.Kind, MatchKey: s.MatchKey, From: s.From, To: s.To, ImageHash: s.ImageHash,
		ImageMime: s.ImageMime, AlbumName: s.AlbumName, AlbumArtist: s.AlbumArtist, Year: s.Year,
		TrackNumbers: s.TrackNumbers, MbzAlbumID: s.MbzAlbumID, MbzReleaseGroupID: s.MbzReleaseGroupID}
}

func argsOf(job *model.EditJob) (writeArgs, error) {
	var a writeArgs
	if err := json.Unmarshal([]byte(job.Args), &a); err != nil {
		return a, fmt.Errorf("reading the job's args: %w", err)
	}
	return a, nil
}

func (a writeArgs) String() string {
	b, _ := json.Marshal(a)
	return string(b)
}

// PlanAlbumEdit works out the job that writes the album Edit dialog's values into the album's
// Files: the title, album artist and year of fix (nil when only the numbers changed), into every
// File, and each Track's disc and track numbers, into its File. A value is written where it
// differs from the album's, or where an album Correction overrides it (then the album shows the
// Correction's value, which the Files get, and the Correction loses it once they have). A new
// album artist takes the place of the File's album artists, as an album Correction's does. The
// MusicBrainz release fix picked is not written here (see CorrectAlbum).
func (c *corrections) PlanAlbumEdit(ctx context.Context, albumID string, fix *AlbumFix, numbers []TrackNumber) (*Plan, error) {
	e, err := c.albumEdit(ctx, albumID, fix, numbers)
	if err != nil {
		return nil, err
	}
	plan := &Plan{Kind: JobTrackNumbers, TrackID: e.tracks[0].ID}
	if e.args.Name || e.args.AlbumArtist || e.args.Year {
		plan.Kind = JobAlbumEdit
	}
	for _, mf := range e.tracks {
		r := e.rewrites[mf.ID]
		if len(r.Set) == 0 && r.Year == 0 {
			continue
		}
		step, err := editjob.RewriteStep(mf.ID, r)
		if err != nil {
			return nil, err
		}
		plan.Steps = append(plan.Steps, step)
	}
	if len(plan.Steps) == 0 {
		return nil, ErrNothingToWrite
	}
	plan.Args = e.args.String()
	return plan, nil
}

// albumEditing is what the album Edit dialog's values write into an album's Files: the album,
// its Tracks (in album order), the Rewrite of each (by Track ID; with nothing to write where the
// File keeps all it holds), and the job's Args.
type albumEditing struct {
	al       *model.Album
	tracks   model.MediaFiles
	rewrites map[string]editjob.Rewrite
	args     writeArgs
}

// albumEdit works out what the album Edit dialog's values write into the album's Files (see
// PlanAlbumEdit), refusing what can't be written, but not a save that writes nothing.
func (c *corrections) albumEdit(ctx context.Context, albumID string, fix *AlbumFix, numbers []TrackNumber) (*albumEditing, error) {
	al, err := c.ds.Album(ctx).Get(albumID)
	if err != nil {
		return nil, err
	}
	if len(al.FolderIDs) == 0 {
		return nil, fmt.Errorf("%w: the album has no folder", model.ErrValidation)
	}
	tracks, err := c.ds.MediaFile(ctx).GetAll(model.QueryOptions{Sort: "album", Filters: squirrel.Eq{
		"media_file.album_id": al.ID, "media_file.missing": false}})
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("%w: the album has no Tracks", model.ErrValidation)
	}
	repo := c.ds.Correction(ctx)
	a := writeArgs{Folders: slices.Clone(al.FolderIDs)}
	var albumFix editjob.Rewrite
	if fix != nil {
		if err := cleanAlbumFix(fix); err != nil {
			return nil, err
		}
		old, err := model.AlbumCorrection(repo, model.CorrectionAlbum, *al)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return nil, err
		}
		if old == nil {
			old = &model.Correction{}
		}
		albumFix.Set = tagedit.Values{}
		if fix.Name != al.Name || old.AlbumName != "" {
			a.Name, albumFix.Set["album"] = true, []string{fix.Name}
		}
		if fix.AlbumArtist != al.AlbumArtist || old.AlbumArtist != "" {
			if fix.AlbumArtist != al.AlbumArtist {
				if _, err := c.artistCorrection(ctx, model.ArtistNameKey(fix.AlbumArtist)); err == nil {
					return nil, ErrRenamedAway
				} else if !errors.Is(err, model.ErrNotFound) {
					return nil, err
				}
			}
			a.AlbumArtist, albumFix.Set["albumartist"] = true, []string{fix.AlbumArtist}
			albumFix.Drop = map[string][]string{"albumartist": newAlbumArtistDrops}
		}
		if fix.Year != 0 && (fix.Year != albumYear(*al) || old.Year != 0) {
			a.Year, albumFix.Year = true, fix.Year
		}
	}

	byID := map[string]int{}
	for i, mf := range tracks {
		byID[mf.ID] = i
	}
	numbered := map[string]tagedit.Values{}
	for _, t := range numbers {
		i, ok := byID[t.ID]
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: %s is no Track of the album", model.ErrValidation, t.ID)
		case numbered[t.ID] != nil:
			return nil, fmt.Errorf("%w: %s is listed twice", model.ErrValidation, t.ID)
		}
		mf := tracks[i]
		// A Track with no disc number may keep none; otherwise the numbers must be numbers.
		if t.TrackNumber < 1 || t.TrackNumber > maxTrackNumber || t.DiscNumber < 0 || t.DiscNumber > maxDiscNumber ||
			(t.DiscNumber == 0 && mf.DiscNumber != 0) {
			return nil, fmt.Errorf("%w: %d-%d is no track number", model.ErrValidation, t.DiscNumber, t.TrackNumber)
		}
		vals := tagedit.Values{"tracknumber": {strconv.Itoa(t.TrackNumber)}}
		if t.DiscNumber > 0 {
			vals["discnumber"] = []string{strconv.Itoa(t.DiscNumber)}
		}
		numbered[t.ID] = vals
		a.Numbered = append(a.Numbered, fileRef{TrackID: mf.ID, LibraryID: mf.LibraryID, Path: mf.Path})
	}

	e := &albumEditing{al: al, tracks: tracks, rewrites: map[string]editjob.Rewrite{}}
	for _, mf := range tracks {
		r := albumFix
		r.Set = tagedit.Values{}
		maps.Copy(r.Set, albumFix.Set)
		maps.Copy(r.Set, numbered[mf.ID])
		e.rewrites[mf.ID] = r
	}
	e.args = a
	return e, nil
}

// newAlbumArtistDrops are the fields a new album artist takes the place of, as an album
// Correction's does: the album artists, and their MusicBrainz IDs.
var newAlbumArtistDrops = []string{"albumartists", "musicbrainz_albumartistid"}

// cleanAlbumFix folds the album Edit dialog's values as CorrectAlbum does, and checks them.
func cleanAlbumFix(fix *AlbumFix) error {
	fix.Name = strings.TrimSpace(fix.Name)
	fix.AlbumArtist = strings.Join(strings.Fields(fix.AlbumArtist), " ")
	switch {
	case fix.Name == "":
		return fmt.Errorf("%w: the title is empty", model.ErrValidation)
	case fix.AlbumArtist == "":
		return fmt.Errorf("%w: the album artist is empty", model.ErrValidation)
	case fix.Year != 0 && (fix.Year < 1000 || fix.Year > 9999):
		return fmt.Errorf("%w: %d is no year", model.ErrValidation, fix.Year)
	}
	return nil
}

// PlanArtistRename works out the job that renames the Artist to name in every File that names it
// as Track artist or album artist, refusing what RenameArtist refuses: a name another Artist has
// (ErrArtistExists: that is a merge, PlanArtistMerge), and a name a Correction renames away.
func (c *corrections) PlanArtistRename(ctx context.Context, artistID, name string) (*Plan, error) {
	name = strings.Join(strings.Fields(name), " ")
	ar, err := c.ds.Artist(ctx).Get(artistID)
	if err != nil {
		return nil, err
	}
	switch {
	case name == "":
		return nil, fmt.Errorf("%w: the new name is empty", model.ErrValidation)
	case name == ar.Name:
		return nil, fmt.Errorf("%w: the name is unchanged", model.ErrValidation)
	case isPlaceholder(ar.Name):
		return nil, fmt.Errorf("%w: %s can't be renamed", model.ErrValidation, ar.Name)
	}
	if newID := metadata.ArtistID(name); newID != ar.ID {
		if _, err := c.ds.Artist(ctx).Get(newID); err == nil {
			return nil, ErrArtistExists
		} else if !errors.Is(err, model.ErrNotFound) {
			return nil, err
		}
	}
	return c.planArtist(ctx, ar, name, false)
}

// PlanArtistMerge works out the job that renames the Artist to the name of the Artist intoID in
// every File that names it, merging the two.
func (c *corrections) PlanArtistMerge(ctx context.Context, artistID, intoID string) (*Plan, error) {
	ar, err := c.ds.Artist(ctx).Get(artistID)
	if err != nil {
		return nil, err
	}
	into, err := c.ds.Artist(ctx).Get(intoID)
	if err != nil {
		return nil, err
	}
	switch {
	case ar.ID == into.ID:
		return nil, fmt.Errorf("%w: an Artist can't be merged into itself", model.ErrValidation)
	case isPlaceholder(ar.Name) || isPlaceholder(into.Name):
		return nil, fmt.Errorf("%w: %s and %s can't be merged", model.ErrValidation, ar.Name, into.Name)
	}
	return c.planArtist(ctx, ar, into.Name, true)
}

// planArtist works out the job renaming the Artist ar to name in its Files. The Files may name
// it by other names that artist rename Corrections rename to it: those are renamed too. A Track
// whose album artist an album Correction sets to the Artist gets the new name as album artist.
func (c *corrections) planArtist(ctx context.Context, ar *model.Artist, name string, merge bool) (*Plan, error) {
	if _, err := c.artistCorrection(ctx, model.ArtistNameKey(name)); err == nil {
		return nil, ErrRenamedAway
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	all, err := c.ds.Correction(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	names := []string{ar.Name}
	keys := map[string]bool{model.ArtistNameKey(ar.Name): true}
	for added := true; added; {
		added = false
		for _, cr := range all {
			if !cr.Kind.IsArtistRename() || cr.Undoing || !keys[model.ArtistNameKey(cr.To)] || keys[cr.MatchKey] {
				continue
			}
			names, keys[cr.MatchKey], added = append(names, cr.From), true, true
		}
	}
	// The folders of the albums whose album Corrections (not yet migrated, #165) set the Artist.
	albumArtistFixes := map[string]bool{}
	for _, cr := range all {
		if cr.Kind == model.CorrectionAlbum && !cr.Undoing && cr.AlbumArtist != "" && keys[model.ArtistNameKey(cr.AlbumArtist)] {
			folders, err := c.ds.Correction(ctx).AlbumFolderIDs(cr.MatchKey)
			if err != nil {
				return nil, err
			}
			for _, f := range folders {
				albumArtistFixes[f] = true
			}
		}
	}

	tracks, err := c.ds.MediaFile(ctx).GetAll(model.QueryOptions{Sort: "album", Filters: squirrel.And{
		persistence.ParticipantIDFilter("media_file", ar.ID, model.RoleArtist, model.RoleAlbumArtist),
		squirrel.Eq{"media_file.missing": false},
	}})
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("%w: no Track names the Artist", model.ErrValidation)
	}
	rename := editjob.Rewrite{Rename: &editjob.Rename{From: names, To: name}}
	plan := &Plan{Kind: JobArtistRename, ArtistID: metadata.ArtistID(name)}
	if merge {
		plan.Kind = JobArtistMerge
	}
	for _, mf := range tracks {
		r := rename
		if albumArtistFixes[mf.FolderID] {
			r.Set = tagedit.Values{"albumartist": {name}}
			r.Drop = map[string][]string{"albumartist": newAlbumArtistDrops}
		}
		step, err := editjob.RewriteStep(mf.ID, r)
		if err != nil {
			return nil, err
		}
		plan.Steps = append(plan.Steps, step)
	}
	plan.Args = writeArgs{FromID: ar.ID, ToID: plan.ArtistID, Names: names, To: name, Merge: merge}.String()
	return plan, nil
}

// JobKinds are the job kinds above, and the Correction migration's (JobMigrate), for the edit job
// queue (editjob.Queue.Handle).
func JobKinds(ds model.DataStore) map[model.EditKind]editjob.JobKind {
	w := writeKind{ds: ds}
	return map[model.EditKind]editjob.JobKind{
		JobAlbumEdit:    albumKind{w},
		JobTrackNumbers: albumKind{w},
		JobArtistRename: artistKind{w},
		JobArtistMerge:  artistKind{w},
		JobMigrate:      migrateKind{ds: ds},
	}
}

type writeKind struct {
	ds model.DataStore
}

// written returns the Files a job's steps left holding what they write: written, or unchanged.
func written(steps model.EditJobSteps) map[string]bool {
	res := map[string]bool{}
	for _, s := range steps {
		if s.State == model.EditStepWritten || s.State == model.EditStepUnchanged {
			res[s.MediaFileID] = true
		}
	}
	return res
}

// anySkipped reports whether a step of the job wasn't written: its File still holds what the
// Corrections fix.
func anySkipped(steps model.EditJobSteps) bool {
	return slices.ContainsFunc(steps, func(s model.EditJobStep) bool { return s.State == model.EditStepSkipped })
}

// change changes Corrections in a transaction (edit, which returns whether it changed any), and
// keeps what it noted in the job's Args with them. Nothing applies a Correction on read any more
// (#165), so nothing is rescanned.
func (w writeKind) change(ctx context.Context, job *model.EditJob, a *writeArgs, edit func(tx model.DataStore) (bool, error)) error {
	return w.ds.WithTx(func(tx model.DataStore) error {
		if _, err := edit(tx); err != nil {
			return err
		}
		job.Args = a.String()
		return tx.EditJob(ctx).Put(job)
	})
}

// restore puts back the Corrections a job removed, in place of what is there now.
func restore(ctx context.Context, tx model.DataStore, saved []savedCorrection) error {
	repo := tx.Correction(ctx)
	for _, s := range saved {
		if cur, err := repo.GetByMatchKey(s.Kind, s.MatchKey); err == nil {
			if err := repo.Delete(cur.ID); err != nil {
				return err
			}
		} else if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		cr := s.correction()
		if err := repo.Add(&cr); err != nil {
			return err
		}
	}
	return nil
}

func (w writeKind) Undone(ctx context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	a, err := argsOf(job)
	if err != nil || len(written(steps)) == 0 {
		return err
	}
	if len(a.Restore) == 0 {
		return nil
	}
	saved := a.Restore
	a.Restore = nil
	log.Info(ctx, "Corrections: putting back the Corrections an undone edit job removed", "job", job.ID, "corrections", len(saved))
	return w.change(ctx, job, &a, func(tx model.DataStore) (bool, error) {
		return true, restore(ctx, tx, saved)
	})
}

// albumKind removes, once an album job's Files are written, what the album's Corrections override
// of what the job wrote: the album Correction's title, album artist and year, and the track
// numbers Correction's fixes of the Files it numbered.
type albumKind struct{ writeKind }

func (k albumKind) Start(context.Context, *model.EditJob) error { return nil }

func (k albumKind) Done(ctx context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	a, err := argsOf(job)
	if err != nil {
		return err
	}
	ok := written(steps)
	if len(ok) == 0 {
		return nil
	}
	return k.change(ctx, job, &a, func(tx model.DataStore) (bool, error) {
		repo := tx.Correction(ctx)
		var saved []savedCorrection
		// The album Correction applies to all the album's Files: a File the job skipped still
		// needs it, so it stays whole.
		if (a.Name || a.AlbumArtist || a.Year || a.Release) && !anySkipped(steps) {
			albums, err := repo.FindByMatchKeys(a.Folders, model.CorrectionAlbum)
			if err != nil {
				return false, err
			}
			for _, cr := range albums {
				trimmed := cr
				if a.Name {
					trimmed.AlbumName = ""
				}
				if a.AlbumArtist {
					trimmed.AlbumArtist = ""
				}
				if a.Year {
					trimmed.Year = 0
				}
				// The release written into the Files takes the place of the one picked before.
				if a.Release {
					trimmed.MbzAlbumID, trimmed.MbzReleaseGroupID, trimmed.ImageHash, trimmed.ImageMime = "", "", "", ""
				}
				if cr.Undoing || (trimmed.AlbumName == cr.AlbumName && trimmed.AlbumArtist == cr.AlbumArtist && trimmed.Year == cr.Year &&
					trimmed.MbzAlbumID == cr.MbzAlbumID && trimmed.ImageHash == cr.ImageHash) {
					continue
				}
				trimmed.To = trimmedAlbumLabel(cr, trimmed)
				if err := replaceCorrection(repo, cr, trimmed, trimmed.AlbumName != "" || trimmed.AlbumArtist != "" ||
					trimmed.Year != 0 || trimmed.MbzAlbumID != ""); err != nil {
					return false, err
				}
				saved = append(saved, save(cr))
			}
		}
		numbered := map[fileRef]bool{}
		for _, f := range a.Numbered {
			if ok[f.TrackID] {
				numbered[fileRef{LibraryID: f.LibraryID, Path: f.Path}] = true
			}
		}
		if len(numbered) > 0 {
			all, err := repo.FindByMatchKeys(a.Folders, model.CorrectionTrackNumbers)
			if err != nil {
				return false, err
			}
			for _, cr := range all {
				trimmed := cr
				trimmed.TrackNumbers = slices.DeleteFunc(slices.Clone(cr.TrackNumbers), func(f model.TrackNumberFix) bool {
					return numbered[fileRef{LibraryID: f.LibraryID, Path: f.Path}]
				})
				if cr.Undoing || len(trimmed.TrackNumbers) == len(cr.TrackNumbers) {
					continue
				}
				trimmed.To = trackNumbersLabel(trimmed.TrackNumbers)
				if err := replaceCorrection(repo, cr, trimmed, len(trimmed.TrackNumbers) > 0); err != nil {
					return false, err
				}
				saved = append(saved, save(cr))
			}
		}
		if len(saved) == 0 {
			return false, nil
		}
		log.Info(ctx, "Corrections: removed what the album's Corrections override of what an edit job wrote", "job", job.ID, "corrections", len(saved))
		a.Restore = saved
		return true, nil
	})
}

// trimmedAlbumLabel names what is left of the album Correction cr once trimmed, for the
// Corrections page: what it still sets ("Title", "Band", "1999", joined), and its picked release.
func trimmedAlbumLabel(cr, trimmed model.Correction) string {
	var parts []string
	for _, p := range []string{trimmed.AlbumArtist, trimmed.AlbumName} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	label := strings.Join(parts, " – ")
	if trimmed.Year != 0 {
		label = strings.TrimSpace(fmt.Sprintf("%s (%d)", label, trimmed.Year))
	}
	if i := strings.Index(cr.To, mbzLabel); i >= 0 && trimmed.MbzAlbumID != "" {
		if label == "" {
			return strings.TrimPrefix(cr.To[i:], " · ")
		}
		label += cr.To[i:]
	}
	return label
}

// replaceCorrection replaces the Correction cr by trimmed (what is left of it), or deletes it when
// nothing is left (keep false).
func replaceCorrection(repo model.CorrectionRepository, cr, trimmed model.Correction, keep bool) error {
	if err := repo.Delete(cr.ID); err != nil {
		return err
	}
	if !keep {
		return nil
	}
	trimmed.ID = ""
	return repo.Add(&trimmed)
}

// artistKind moves an Artist's stars, ratings and play counts to its new name (kept as the job
// starts, merged once its Files are written, split again by its undo), and removes the
// Corrections its Files no longer need: the artist renames of the names the job renamed, and the
// album artist of album Corrections setting the Artist.
type artistKind struct{ writeKind }

func (k artistKind) Start(ctx context.Context, job *model.EditJob) error {
	a, err := argsOf(job)
	if err != nil || a.FromID == a.ToID {
		return err
	}
	return k.ds.EditJob(ctx).KeepArtistAnnotations(job.ID, a.FromID)
}

func (k artistKind) Done(ctx context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	a, err := argsOf(job)
	if err != nil {
		return err
	}
	if len(written(steps)) == 0 {
		return nil
	}
	keys := map[string]bool{}
	for _, n := range a.Names {
		keys[model.ArtistNameKey(n)] = true
	}
	return k.change(ctx, job, &a, func(tx model.DataStore) (bool, error) {
		if a.FromID != a.ToID {
			if err := tx.EditJob(ctx).MergeArtistAnnotations(job.ID, a.FromID, a.ToID); err != nil {
				return false, err
			}
		}
		repo := tx.Correction(ctx)
		all, err := repo.GetAll()
		if err != nil {
			return false, err
		}
		var saved []savedCorrection
		for _, cr := range all {
			// A File the job skipped still holds the old names: the Corrections it needs stay.
			if cr.Undoing || anySkipped(steps) {
				continue
			}
			switch {
			// An artist merge Correction keeps the stars and ratings it merged; once the Files
			// no longer hold its name it applies to nothing, and the Correction migration (#165)
			// removes it, keeping them for its own undo.
			case cr.Kind == model.CorrectionArtistRename && keys[cr.MatchKey]:
				if err := repo.Delete(cr.ID); err != nil {
					return false, err
				}
			case cr.Kind == model.CorrectionAlbum && cr.AlbumArtist != "" && keys[model.ArtistNameKey(cr.AlbumArtist)]:
				trimmed := cr
				trimmed.AlbumArtist = ""
				trimmed.To = trimmedAlbumLabel(cr, trimmed)
				if err := replaceCorrection(repo, cr, trimmed, trimmed.AlbumName != "" || trimmed.Year != 0 ||
					trimmed.MbzAlbumID != ""); err != nil {
					return false, err
				}
			default:
				continue
			}
			saved = append(saved, save(cr))
		}
		if len(saved) == 0 {
			return false, nil
		}
		log.Info(ctx, "Corrections: removed the Corrections an artist rename job made stale", "job", job.ID, "corrections", len(saved))
		a.Restore = saved
		return true, nil
	})
}

func (k artistKind) Undone(ctx context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	a, err := argsOf(job)
	if err != nil || len(written(steps)) == 0 {
		return err
	}
	if a.FromID != a.ToID {
		// A rename's new name is only the Artist's: what it got since goes back with it too.
		if err := k.ds.WithTx(func(tx model.DataStore) error {
			return tx.EditJob(ctx).SplitArtistAnnotations(job.ID, a.FromID, a.ToID, !a.Merge)
		}); err != nil {
			return err
		}
	}
	return k.writeKind.Undone(ctx, job, steps)
}
