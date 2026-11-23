package corrections_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/metadata"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

// statScanner stands in for the scanner after a write: it reads only the written Files' size and
// modification time back into their Tracks, as a rescan would, so the next write's guard passes.
type statScanner struct {
	ds   model.DataStore
	root string
}

func (s statScanner) ScanFolders(ctx context.Context, _ bool, _ []model.ScanTarget) ([]string, error) {
	mfs, err := s.ds.MediaFile(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	for _, mf := range mfs {
		info, err := os.Stat(filepath.Join(s.root, mf.Path))
		if err != nil {
			continue
		}
		if _, err := db.Db().Exec("update media_file set size = ?, updated_at = ? where id = ?", info.Size(), info.ModTime(), mf.ID); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// The Correction migration (#165), through the real edit job queue and write path, on a real
// database and real mp3 Files. The database holds what the retired overlay made of the Files (the
// Tracks' corrected values, and the Corrections each applies), as an upgraded app finds it.
var _ = Describe("Migrating Corrections into the Files", func() {
	var (
		ctx    context.Context
		ds     model.DataStore
		svc    corrections.Corrections
		editor *tagedit.Editor
		q      *editjob.Queue
		libDir string
		dir    string
		folder *model.Folder
		cover  []byte
	)

	const (
		wrongArtist = "Greatful Dead"
		rightArtist = "Grateful Dead"
	)

	tagsOf := func(name string) map[string][]string {
		GinkgoHelper()
		tags, err := taglib.ReadTags(filepath.Join(dir, name))
		Expect(err).ToNot(HaveOccurred())
		return tags
	}
	// addFile writes a File with the tags, and its Track as the overlay left it in the database.
	addFile := func(id, name string, tags map[string][]string, mf model.MediaFile) model.MediaFile {
		GinkgoHelper()
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(p, data, 0o644)).To(Succeed())
		if filepath.Ext(name) == ".mp3" {
			Expect(taglib.WriteTags(p, tags, taglib.Clear)).To(Succeed())
		}
		info, err := os.Stat(p)
		Expect(err).ToNot(HaveOccurred())
		mf.ID, mf.LibraryID, mf.FolderID, mf.Path = id, 1, folder.ID, "Band/Record/"+name
		mf.Suffix, mf.Size, mf.UpdatedAt = filepath.Ext(name)[1:], info.Size(), info.ModTime()
		mf.AlbumID = "al-1"
		Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
		return mf
	}
	addCorrection := func(cr model.Correction) model.Correction {
		GinkgoHelper()
		Expect(ds.Correction(ctx).Add(&cr)).To(Succeed())
		return cr
	}
	credited := func(name string) model.Participants {
		GinkgoHelper()
		ar := model.Artist{ID: metadata.ArtistID(name), Name: name}
		Expect(ds.Artist(ctx).Put(&ar)).To(Succeed())
		p := model.Participants{}
		p.Add(model.RoleArtist, model.Artist{ID: metadata.ArtistID(name), Name: name})
		p.Add(model.RoleAlbumArtist, model.Artist{ID: metadata.ArtistID(name), Name: name})
		return p
	}
	remaining := func() model.Corrections {
		GinkgoHelper()
		all, err := ds.Correction(ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		return all
	}
	run := func(job *model.EditJob) *model.EditJob {
		GinkgoHelper()
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { q.Run(runCtx); close(done) }()
		defer func() { cancel(); Eventually(done).Should(BeClosed()) }()
		got, err := q.Wait(ctx, job.ID, 20*time.Second)
		Expect(err).ToNot(HaveOccurred())
		Expect(got.State).To(Equal(model.EditJobDone))
		return got
	}
	migrate := func() *model.EditJob {
		GinkgoHelper()
		plan, err := svc.MigrationJob(ctx, editor)
		Expect(err).ToNot(HaveOccurred())
		Expect(plan.Kind).To(Equal(corrections.JobMigrate))
		job, err := q.EnqueueWith(ctx, plan.Kind, plan.Args, plan.Steps)
		Expect(err).ToNot(HaveOccurred())
		return run(job)
	}
	undo := func(job *model.EditJob) *model.EditJob {
		GinkgoHelper()
		steps, err := editjob.UndoJobSteps(ctx, ds, job.ID)
		Expect(err).ToNot(HaveOccurred())
		u, err := q.EnqueueWith(ctx, model.EditUndo, editjob.UndoJobArgs(job.ID), steps)
		Expect(err).ToNot(HaveOccurred())
		return run(u)
	}
	itemOf := func(m *corrections.Migration, kind model.CorrectionKind) corrections.MigrationItem {
		GinkgoHelper()
		for _, it := range m.Items {
			if it.Correction.Kind == kind {
				return it
			}
		}
		Fail("no item of kind " + string(kind))
		return corrections.MigrationItem{}
	}

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-id", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"correction", "correction_annotation", "media_file", "folder", "album", "library_artist",
			"artist", "edit_journal", "edit_job_step", "edit_job", "edit_job_annotation", "annotation"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		libDir = GinkgoT().TempDir()
		_, err := db.Db().Exec("update library set path = ? where id = 1", libDir)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS", Writable: true}}})
		DeferCleanup(func() { core.SetLibraryEditing(core.LibraryEditing{}) })

		folder = model.NewFolder(model.Library{ID: 1}, "Band/Record")
		Expect(ds.Folder(ctx).Put(folder)).To(Succeed())
		dir = filepath.Join(libDir, "Band", "Record")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		al := model.Album{ID: "al-1", Name: "Record", AlbumArtist: rightArtist, LibraryID: 1, FolderIDs: []string{folder.ID}}
		Expect(ds.Album(ctx).Put(&al)).To(Succeed())
		cover, err = os.ReadFile(filepath.Join("tests", "fixtures", "artist", "an-album", "cover.jpg"))
		Expect(err).ToNot(HaveOccurred())

		svc = corrections.NewWithOptions(ds, corrections.Options{})
		editor = tagedit.New(ds, statScanner{ds: ds, root: libDir})
		q = editjob.New(ds, nil, editjob.Kinds(ds, editor))
		for kind, k := range corrections.JobKinds(ds) {
			q.Handle(kind, k)
		}
	})

	// The Library as the overlay showed it: "Greatful Dead – Recrod (1998)" renamed, retitled and
	// dated, Two numbered 2 (tagged 3), and a chosen cover.
	var rename, album, numbers model.Correction
	setUpLibrary := func() {
		GinkgoHelper()
		rename = addCorrection(model.Correction{Kind: model.CorrectionArtistRename, MatchKey: model.ArtistNameKey(wrongArtist),
			From: wrongArtist, To: rightArtist})
		album = addCorrection(model.Correction{Kind: model.CorrectionAlbum, MatchKey: folder.ID, From: "Greatful Dead – Recrod (1998)",
			To: "Greatful Dead – Record (1999)", AlbumName: "Record", Year: 1999})
		numbers = addCorrection(model.Correction{Kind: model.CorrectionTrackNumbers, MatchKey: folder.ID, From: "Record",
			To: "2 Two", TrackNumbers: model.TrackNumberFixes{{LibraryID: 1, Path: "Band/Record/02 Two.mp3", Title: "Two",
				FromTrack: 3, Track: 2}}})
		stored, err := artwork.StoreChosenCover(cover)
		Expect(err).ToNot(HaveOccurred())
		addCorrection(model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: folder.ID, From: "Record",
			To: "Upload", ImageHash: stored.Hash, ImageMime: stored.Mime})
		tags := func(title, track string) map[string][]string {
			return map[string][]string{"TITLE": {title}, "ARTIST": {wrongArtist}, "ALBUMARTIST": {wrongArtist},
				"ALBUM": {"Recrod"}, "DATE": {"1998-05-01"}, "TRACKNUMBER": {track}}
		}
		addFile("mf-1", "01 One.mp3", tags("One", "1"), model.MediaFile{Title: "One", Album: "Record", Artist: rightArtist,
			AlbumArtist: rightArtist, TrackNumber: 1, Participants: credited(rightArtist),
			CorrectionIDs: []string{album.ID, rename.ID}})
		addFile("mf-2", "02 Two.mp3", tags("Two", "3"), model.MediaFile{Title: "Two", Album: "Record", Artist: rightArtist,
			AlbumArtist: rightArtist, TrackNumber: 2, Participants: credited(rightArtist),
			CorrectionIDs: []string{album.ID, numbers.ID, rename.ID}})
	}

	Describe("the plan", func() {
		It("lists every tag and cover Correction with what writing it changes, and how many Files", func() {
			setUpLibrary()
			// These stay app data: an image marked wrong, and a picked MusicBrainz release.
			addCorrection(model.Correction{Kind: model.CorrectionAlbumCoverBlock, MatchKey: folder.ID, ImageHash: "abc"})

			m, err := svc.PlanMigration(ctx, editor)
			Expect(err).ToNot(HaveOccurred())
			Expect(m.Items).To(HaveLen(4))
			Expect(m.Files).To(Equal(3), "two Tracks and the cover")

			it := itemOf(m, model.CorrectionArtistRename)
			Expect(it.Files).To(Equal(2))
			Expect(it.Changes).To(ConsistOf(corrections.MigrationChange{Field: "artist", From: wrongArtist, To: rightArtist}))
			Expect(it.Flags).To(BeEmpty())

			it = itemOf(m, model.CorrectionAlbum)
			Expect(it.Files).To(Equal(2))
			Expect(it.Changes).To(ConsistOf(corrections.MigrationChange{Field: "album", To: "Record"},
				corrections.MigrationChange{Field: "year", To: "1999"}))

			it = itemOf(m, model.CorrectionTrackNumbers)
			Expect(it.Files).To(Equal(1))
			Expect(it.Changes).To(ConsistOf(corrections.MigrationChange{Field: "tracknumber", From: "3 Two", To: "2"}))

			it = itemOf(m, model.CorrectionAlbumCover)
			Expect(it.Files).To(Equal(1))
			Expect(it.Changes).To(ConsistOf(corrections.MigrationChange{Field: "cover", From: "Upload", To: "Band/Record/cover.jpg"}))

			// Listing writes nothing.
			Expect(tagsOf("01 One.mp3")).To(HaveKeyWithValue("ARTIST", []string{wrongArtist}))
			Expect(filepath.Join(dir, "cover.jpg")).ToNot(BeAnExistingFile())
		})

		It("finds a Track a scan since the upgrade read as tagged, by its tagged artist", func() {
			rename = addCorrection(model.Correction{Kind: model.CorrectionArtistRename, MatchKey: model.ArtistNameKey(wrongArtist),
				From: wrongArtist, To: rightArtist})
			addFile("mf-1", "01 One.mp3", map[string][]string{"ARTIST": {wrongArtist}}, model.MediaFile{Title: "One",
				Artist: wrongArtist, Participants: credited(wrongArtist)})
			m, err := svc.PlanMigration(ctx, editor)
			Expect(err).ToNot(HaveOccurred())
			Expect(itemOf(m, model.CorrectionArtistRename).Files).To(Equal(1))
		})

		It("flags the Files it can't write: changed on disk, missing, or of a kind it can't check", func() {
			setUpLibrary()
			Expect(os.Chtimes(filepath.Join(dir, "01 One.mp3"), time.Now(), time.Now().Add(time.Hour))).To(Succeed())
			_, err := db.Db().Exec("update media_file set missing = true where id = 'mf-2'")
			Expect(err).ToNot(HaveOccurred())
			addFile("mf-3", "03 Three.flac", map[string][]string{"ARTIST": {wrongArtist}}, model.MediaFile{Title: "Three",
				Participants: credited(rightArtist), CorrectionIDs: []string{rename.ID}})

			m, err := svc.PlanMigration(ctx, editor)
			Expect(err).ToNot(HaveOccurred())
			Expect(itemOf(m, model.CorrectionArtistRename).Flags).To(ConsistOf(
				And(HaveField("Path", "Band/Record/01 One.mp3"), HaveField("Code", "changedOnDisk")),
				And(HaveField("Path", "Band/Record/02 Two.mp3"), HaveField("Code", "missing")),
				And(HaveField("Path", "Band/Record/03 Three.flac"), HaveField("Code", "unsupportedFormat")),
			))
			Expect(itemOf(m, model.CorrectionTrackNumbers).Flags).To(ConsistOf(HaveField("Code", "missing")))
			Expect(m.Files).To(Equal(1), "only the cover can be written")
		})
	})

	Describe("the job", func() {
		It("writes every Correction into its Files as one job, and removes each once its Files are written", func() {
			setUpLibrary()
			job := migrate()
			Expect(job.Kind).To(Equal(corrections.JobMigrate))
			Expect(job.Skipped).To(BeZero())
			Expect(job.Steps).To(HaveLen(3))

			one, two := tagsOf("01 One.mp3"), tagsOf("02 Two.mp3")
			for _, tags := range []map[string][]string{one, two} {
				Expect(tags).To(HaveKeyWithValue("ARTIST", []string{rightArtist}))
				Expect(tags).To(HaveKeyWithValue("ALBUMARTIST", []string{rightArtist}))
				Expect(tags).To(HaveKeyWithValue("ALBUM", []string{"Record"}))
				Expect(tags).To(HaveKeyWithValue("DATE", []string{"1999"}))
			}
			Expect(one).To(HaveKeyWithValue("TRACKNUMBER", []string{"1"}))
			Expect(two).To(HaveKeyWithValue("TRACKNUMBER", []string{"2"}))
			Expect(two).To(HaveKeyWithValue("TITLE", []string{"Two"}))
			written, err := os.ReadFile(filepath.Join(dir, "cover.jpg"))
			Expect(err).ToNot(HaveOccurred())
			Expect(written).To(Equal(cover))

			Expect(remaining()).To(BeEmpty())
			m, err := svc.PlanMigration(ctx, editor)
			Expect(err).ToNot(HaveOccurred())
			Expect(m.Items).To(BeEmpty())
		})

		It("writes a picked MusicBrainz release's IDs and its cover into the Files, and removes the Correction (#170)", func() {
			setUpLibrary()
			// No chosen cover: the release's cover is the one the album showed.
			for _, cr := range remaining() {
				if cr.Kind == model.CorrectionAlbumCover || cr.Kind == model.CorrectionAlbum {
					Expect(ds.Correction(ctx).Delete(cr.ID)).To(Succeed())
				}
			}
			stored, err := artwork.StoreChosenCover(cover)
			Expect(err).ToNot(HaveOccurred())
			album = addCorrection(model.Correction{Kind: model.CorrectionAlbum, MatchKey: folder.ID, From: "Recrod",
				To:         "MusicBrainz: Record (GB, CD, 1999)",
				MbzAlbumID: "a7f8ed5e-1111-4a1b-9c1d-123456789abc", MbzReleaseGroupID: "b7f8ed5e-1111-4a1b-9c1d-123456789abc",
				ImageHash: stored.Hash, ImageMime: stored.Mime})
			m, err := svc.PlanMigration(ctx, editor)
			Expect(err).ToNot(HaveOccurred())
			it := itemOf(m, model.CorrectionAlbum)
			Expect(it.Files).To(Equal(3), "two Tracks and the cover")
			Expect(it.Changes).To(ConsistOf(
				corrections.MigrationChange{Field: "musicbrainz_albumid", To: "a7f8ed5e-1111-4a1b-9c1d-123456789abc"},
				corrections.MigrationChange{Field: "musicbrainz_releasegroupid", To: "b7f8ed5e-1111-4a1b-9c1d-123456789abc"},
				corrections.MigrationChange{Field: "cover", From: "Cover Art Archive (MusicBrainz release)", To: "Band/Record/cover.jpg"},
			))

			job := migrate()
			Expect(job.Skipped).To(BeZero())
			for _, name := range []string{"01 One.mp3", "02 Two.mp3"} {
				Expect(tagsOf(name)).To(HaveKeyWithValue("MUSICBRAINZ_ALBUMID", []string{"a7f8ed5e-1111-4a1b-9c1d-123456789abc"}))
				Expect(tagsOf(name)).To(HaveKeyWithValue("MUSICBRAINZ_RELEASEGROUPID", []string{"b7f8ed5e-1111-4a1b-9c1d-123456789abc"}))
			}
			written, err := os.ReadFile(filepath.Join(dir, "cover.jpg"))
			Expect(err).ToNot(HaveOccurred())
			Expect(written).To(Equal(cover))
			Expect(remaining()).To(BeEmpty())

			// Its undo puts the Correction back, for the migration to list again.
			undo(job)
			Expect(ds.Correction(ctx).GetByMatchKey(model.CorrectionAlbum, folder.ID)).To(HaveField("ImageHash", stored.Hash))
			Expect(tagsOf("01 One.mp3")).ToNot(HaveKey("MUSICBRAINZ_ALBUMID"))
			Expect(filepath.Join(dir, "cover.jpg")).ToNot(BeAnExistingFile())
		})

		It("leaves a Correction whose Files couldn't all be written, with the reason, and writes the rest", func() {
			setUpLibrary()
			plan, err := svc.MigrationJob(ctx, editor)
			Expect(err).ToNot(HaveOccurred())
			job, err := q.EnqueueWith(ctx, plan.Kind, plan.Args, plan.Steps)
			Expect(err).ToNot(HaveOccurred())
			// One gets a second name after the plan: the write path refuses it, as a copy would
			// split it from that name.
			other := filepath.Join(libDir, "elsewhere.mp3")
			Expect(os.Link(filepath.Join(dir, "01 One.mp3"), other)).To(Succeed())
			job = run(job)
			Expect(job.Skipped).To(Equal(1))

			Expect(tagsOf("01 One.mp3")).To(HaveKeyWithValue("ARTIST", []string{wrongArtist}))
			Expect(tagsOf("02 Two.mp3")).To(HaveKeyWithValue("ARTIST", []string{rightArtist}))
			// The rename and the album Correction apply to One too, so they stay; Two's numbers
			// and the cover are written, so those go.
			Expect(remaining()).To(ConsistOf(HaveField("Kind", model.CorrectionArtistRename), HaveField("Kind", model.CorrectionAlbum)))

			m, err := svc.PlanMigration(ctx, editor)
			Expect(err).ToNot(HaveOccurred())
			Expect(itemOf(m, model.CorrectionArtistRename).LastRun).To(ConsistOf(
				And(HaveField("Path", "Band/Record/01 One.mp3"), HaveField("Code", "linked"))))

			// Once the other name is gone, running the migration again finishes it.
			Expect(os.Remove(other)).To(Succeed())
			migrate()
			Expect(tagsOf("01 One.mp3")).To(HaveKeyWithValue("ARTIST", []string{rightArtist}))
			Expect(remaining()).To(BeEmpty())
		})

		It("writes a picked release into none of the album's Files when one of them can't be written, so it isn't split", func() {
			setUpLibrary()
			_, err := db.Db().Exec("update correction set mbz_album_id = 'a7f8ed5e-1111-4a1b-9c1d-123456789abc' where id = ?", album.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(os.Chtimes(filepath.Join(dir, "01 One.mp3"), time.Now(), time.Now().Add(time.Hour))).To(Succeed())
			migrate()
			Expect(tagsOf("02 Two.mp3")).ToNot(HaveKey("MUSICBRAINZ_ALBUMID"))
			Expect(tagsOf("02 Two.mp3")).To(HaveKeyWithValue("ALBUM", []string{"Record"}))
			Expect(ds.Correction(ctx).GetByMatchKey(model.CorrectionAlbum, folder.ID)).To(
				HaveField("MbzAlbumID", "a7f8ed5e-1111-4a1b-9c1d-123456789abc"), "it stays, to be written once One can be")
		})

		It("writes the chosen cover, not the picked release's, when the album has both", func() {
			setUpLibrary()
			_, err := db.Db().Exec("update correction set mbz_album_id = 'a7f8ed5e-1111-4a1b-9c1d-123456789abc', image_hash = 'pick', image_mime = 'image/jpeg' where id = ?", album.ID)
			Expect(err).ToNot(HaveOccurred())
			migrate()
			Expect(remaining()).To(BeEmpty())
			written, err := os.ReadFile(filepath.Join(dir, "cover.jpg"))
			Expect(err).ToNot(HaveOccurred())
			Expect(written).To(Equal(cover), "the chosen cover outranked the release's")
			Expect(tagsOf("01 One.mp3")).To(HaveKeyWithValue("MUSICBRAINZ_ALBUMID", []string{"a7f8ed5e-1111-4a1b-9c1d-123456789abc"}))
		})

		It("writes each File the album Correction the overlay applied to its folder", func() {
			// Two discs of one album, each folder keying a Correction of its own.
			cd2 := model.NewFolder(model.Library{ID: 1}, "Band/Record/CD2")
			Expect(ds.Folder(ctx).Put(cd2)).To(Succeed())
			_, err := db.Db().Exec("update album set folder_ids = ? where id = 'al-1'", `["`+folder.ID+`","`+cd2.ID+`"]`)
			Expect(err).ToNot(HaveOccurred())
			older := addCorrection(model.Correction{Kind: model.CorrectionAlbum, MatchKey: folder.ID, AlbumName: "X"})
			newer := addCorrection(model.Correction{Kind: model.CorrectionAlbum, MatchKey: cd2.ID, AlbumName: "Y"})
			addFile("mf-1", "01 One.mp3", map[string][]string{"ALBUM": {"Recrod"}}, model.MediaFile{Title: "One"})
			Expect(os.MkdirAll(filepath.Join(dir, "CD2"), 0o755)).To(Succeed())
			folder2 := folder
			folder = cd2
			addFile("mf-2", "CD2/02 Two.mp3", map[string][]string{"ALBUM": {"Recrod"}}, model.MediaFile{Title: "Two"})
			folder = folder2

			m, err := svc.PlanMigration(ctx, editor)
			Expect(err).ToNot(HaveOccurred())
			files := map[string]int{}
			for _, it := range m.Items {
				files[it.Correction.ID] = it.Files
			}
			Expect(files).To(Equal(map[string]int{older.ID: 1, newer.ID: 1}))
			migrate()
			Expect(tagsOf("01 One.mp3")).To(HaveKeyWithValue("ALBUM", []string{"X"}))
			Expect(tagsOf("CD2/02 Two.mp3")).To(HaveKeyWithValue("ALBUM", []string{"Y"}))
		})

		It("writes one cover for two choices keyed by two folders of one album", func() {
			cd1 := model.NewFolder(model.Library{ID: 1}, "Band/Record/CD1")
			cd2 := model.NewFolder(model.Library{ID: 1}, "Band/Record/CD2")
			Expect(ds.Folder(ctx).Put(cd1)).To(Succeed())
			Expect(ds.Folder(ctx).Put(cd2)).To(Succeed())
			_, err := db.Db().Exec("update album set folder_ids = ? where id = 'al-1'", `["`+cd1.ID+`","`+cd2.ID+`"]`)
			Expect(err).ToNot(HaveOccurred())
			for _, f := range []*model.Folder{cd1, cd2} {
				Expect(os.MkdirAll(filepath.Join(libDir, f.Path, f.Name), 0o755)).To(Succeed())
				folder = f
				addFile("mf-"+f.Name, f.Name+"/01 One.mp3", map[string][]string{"ALBUM": {"Record"}}, model.MediaFile{Title: "One"})
			}
			stored, err := artwork.StoreChosenCover(cover)
			Expect(err).ToNot(HaveOccurred())
			for _, f := range []*model.Folder{cd1, cd2} {
				addCorrection(model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: f.ID, From: "Record", To: "Upload",
					ImageHash: stored.Hash, ImageMime: stored.Mime})
			}
			job := migrate()
			Expect(job.Steps).To(HaveLen(1))
			Expect(filepath.Join(dir, "cover.jpg")).To(BeAnExistingFile())
			Expect(remaining()).To(BeEmpty())
		})

		It("removes a Correction that applies to no File any more, with nothing to write", func() {
			addCorrection(model.Correction{Kind: model.CorrectionArtistMerge, MatchKey: model.ArtistNameKey("Gone"),
				From: "Gone", To: rightArtist})
			_, err := svc.MigrationJob(ctx, editor)
			Expect(err).To(MatchError(corrections.ErrNothingToWrite))
			Expect(remaining()).To(BeEmpty())
		})

		It("is undone from the Edit history as a whole: the Files and the Corrections come back", func() {
			setUpLibrary()
			before1, before2 := tagsOf("01 One.mp3"), tagsOf("02 Two.mp3")
			job := migrate()
			u := undo(job)
			Expect(u.Skipped).To(BeZero())

			Expect(tagsOf("01 One.mp3")).To(Equal(before1))
			Expect(tagsOf("02 Two.mp3")).To(Equal(before2))
			Expect(filepath.Join(dir, "cover.jpg")).ToNot(BeAnExistingFile())
			Expect(remaining()).To(HaveLen(4))
			m, err := svc.PlanMigration(ctx, editor)
			Expect(err).ToNot(HaveOccurred())
			Expect(m.Items).To(HaveLen(4))

			// Undoing the undo writes them again, and removes the Corrections again.
			undo(u)
			Expect(tagsOf("01 One.mp3")).To(HaveKeyWithValue("ARTIST", []string{rightArtist}))
			Expect(remaining()).To(BeEmpty())
		})
	})

	Describe("the Artists' stars", func() {
		star := func(artist string, at time.Time) {
			GinkgoHelper()
			_, err := db.Db().Exec(`insert into annotation (user_id, item_id, item_type, starred, starred_at)
				values ('admin-id', ?, 'artist', true, ?)`, metadata.ArtistID(artist), at)
			Expect(err).ToNot(HaveOccurred())
		}
		starred := func(artist string) bool {
			GinkgoHelper()
			var n int
			Expect(db.Db().QueryRow(`select count(*) from annotation where user_id = 'admin-id' and item_type = 'artist'
				and item_id = ? and starred`, metadata.ArtistID(artist)).Scan(&n)).To(Succeed())
			return n > 0
		}
		addArtist := func(name string) {
			GinkgoHelper()
			ar := model.Artist{ID: metadata.ArtistID(name), Name: name}
			Expect(ds.Artist(ctx).Put(&ar)).To(Succeed())
		}
		addOne := func(cr model.Correction) {
			GinkgoHelper()
			addFile("mf-1", "01 One.mp3", map[string][]string{"TITLE": {"One"}, "ARTIST": {wrongArtist}},
				model.MediaFile{Title: "One", Artist: rightArtist, Participants: credited(rightArtist), CorrectionIDs: []string{cr.ID}})
		}

		BeforeEach(func() {
			_, err := db.Db().Exec("insert or ignore into user (id, user_name, name, email, password, is_admin, created_at, updated_at) values ('admin-id', 'admin', 'Admin', '', '', true, datetime(), datetime())")
			Expect(err).ToNot(HaveOccurred())
		})

		It("go back to the old name when a plain rename's migration is undone", func() {
			// The overlay moved the old name's star to the new name, which is an Artist; the old isn't.
			cr := addCorrection(model.Correction{Kind: model.CorrectionArtistRename, MatchKey: model.ArtistNameKey(wrongArtist),
				From: wrongArtist, To: rightArtist})
			addOne(cr)
			addArtist(rightArtist)
			star(rightArtist, time.Now())

			job := migrate()
			Expect(starred(rightArtist)).To(BeTrue())
			undo(job)
			Expect(starred(wrongArtist)).To(BeTrue())
			Expect(starred(rightArtist)).To(BeFalse())
		})

		It("survive a scan before the migration that purged the new name, once kept as the app started", func() {
			cr := addCorrection(model.Correction{Kind: model.CorrectionArtistRename, MatchKey: model.ArtistNameKey(wrongArtist),
				From: wrongArtist, To: rightArtist})
			addOne(cr)
			addArtist(rightArtist)
			star(rightArtist, time.Now())
			Expect(corrections.KeepRenamedStars(ctx, ds)).To(Succeed())
			Expect(corrections.KeepRenamedStars(ctx, ds)).To(Succeed(), "keeping again keeps what was kept")

			// A scan reads the old name: the new one is purged, its star with it.
			_, err := db.Db().Exec("delete from annotation where item_type = 'artist'")
			Expect(err).ToNot(HaveOccurred())
			_, err = db.Db().Exec("delete from artist where id = ?", metadata.ArtistID(rightArtist))
			Expect(err).ToNot(HaveOccurred())

			job := migrate()
			Expect(starred(rightArtist)).To(BeTrue())
			undo(job)
			Expect(starred(wrongArtist)).To(BeTrue())
			Expect(starred(rightArtist)).To(BeFalse())
		})

		It("are split again as an artist merge Correction kept them when its migration is undone", func() {
			// The overlay merged the old name into an Artist with no star, which took the old one's.
			cr := addCorrection(model.Correction{Kind: model.CorrectionArtistMerge, MatchKey: model.ArtistNameKey(wrongArtist),
				From: wrongArtist, To: rightArtist})
			addOne(cr)
			addArtist(rightArtist)
			at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			star(rightArtist, at)
			_, err := db.Db().Exec(`insert into correction_annotation (correction_id, user_id, starred, starred_at, star_taken)
				values (?, 'admin-id', true, ?, true)`, cr.ID, at)
			Expect(err).ToNot(HaveOccurred())

			job := migrate()
			Expect(remaining()).To(BeEmpty())
			Expect(starred(rightArtist)).To(BeTrue())
			undo(job)
			Expect(starred(wrongArtist)).To(BeTrue(), "the old name gets back the star it had")
			Expect(starred(rightArtist)).To(BeFalse(), "the Artist it was merged into had none of its own")
		})
	})
})
