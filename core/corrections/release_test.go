package corrections_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"go.senan.xyz/taglib"
)

// The MusicBrainz IDs of the release in these tests.
const (
	relID   = "8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c"
	groupID = "1b2c3d4e-5f60-4718-9a0b-c1d2e3f40516"
	bandID  = "5b11f4ce-a62d-471e-81fc-a69a8278c7da"
	rec1    = "11111111-1111-4111-8111-111111111111"
	rec2    = "22222222-2222-4222-8222-222222222222"
	rec3    = "33333333-3333-4333-8333-333333333333"
	rt1     = "aaaaaaaa-1111-4111-8111-111111111111"
	rt2     = "aaaaaaaa-2222-4222-8222-222222222222"
	rt3     = "aaaaaaaa-3333-4333-8333-333333333333"
	// otherRec is a recording a File holds already, which the release pairs it with another of.
	otherRec = "99999999-9999-4999-8999-999999999999"
)

// testRelease is a release as musicbrainz.Client.Release answers it: "Band – Record" (1999), three
// tracks on one CD.
func testRelease() *musicbrainz.Release {
	credit := musicbrainz.ArtistCredits{{Name: "Band"}}
	credit[0].Artist.ID, credit[0].Artist.Name = bandID, "Band"
	return &musicbrainz.Release{ID: relID, Title: "Record", Date: "1999-03-02", Country: "GB", ArtistCredit: credit,
		Genres: []musicbrainz.Genre{{Name: "rock", Count: 1}},
		ReleaseGroup: musicbrainz.ReleaseGroup{ID: groupID, Title: "Record", FirstReleaseDate: "1998-11-01",
			Genres: []musicbrainz.Genre{{Name: "alternative rock", Count: 4}, {Name: "rock", Count: 2}}},
		Media: []musicbrainz.Media{{Format: "CD", Position: 1, TrackCount: 3, Tracks: []musicbrainz.Track{
			{ID: rt1, Position: 1, Title: "One", Recording: musicbrainz.Recording{ID: rec1}},
			{ID: rt2, Position: 2, Title: "Two (Remastered)", Recording: musicbrainz.Recording{ID: rec2}},
			{ID: rt3, Position: 3, Title: "Three", Recording: musicbrainz.Recording{ID: rec3}},
		}}}}
}

// Find on MusicBrainz writing the picked release's IDs into the album's Files (#170), through the
// real edit job queue and write path, on a real database and real mp3 Files.
var _ = Describe("Writing a MusicBrainz release into the Files", func() {
	var (
		ctx    context.Context
		ds     model.DataStore
		svc    corrections.Corrections
		editor *tagedit.Editor
		q      *editjob.Queue
		libDir string
		dir    string
		folder *model.Folder
		al     model.Album
	)

	tagsOf := func(name string) map[string][]string {
		GinkgoHelper()
		tags, err := taglib.ReadTags(filepath.Join(dir, name))
		Expect(err).ToNot(HaveOccurred())
		return tags
	}
	addFile := func(id, name string, tags map[string][]string, mf model.MediaFile) {
		GinkgoHelper()
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(p, data, 0o644)).To(Succeed())
		Expect(taglib.WriteTags(p, tags, taglib.Clear)).To(Succeed())
		info, err := os.Stat(p)
		Expect(err).ToNot(HaveOccurred())
		mf.ID, mf.LibraryID, mf.FolderID, mf.Path, mf.AlbumID = id, 1, folder.ID, "Band/Record/"+name, al.ID
		mf.Suffix, mf.Size, mf.UpdatedAt = "mp3", info.Size(), info.ModTime()
		mf.Participants = model.Participants{}
		Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
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
	enqueue := func(plan *corrections.Plan) *model.EditJob {
		GinkgoHelper()
		job, err := q.EnqueueWith(ctx, plan.Kind, plan.Args, plan.Steps)
		Expect(err).ToNot(HaveOccurred())
		return job
	}
	plan := func(w corrections.ReleaseWrite) *corrections.Plan {
		GinkgoHelper()
		p, err := svc.PlanRelease(ctx, editor, al.ID, testRelease(), w)
		Expect(err).ToNot(HaveOccurred())
		Expect(p.Kind).To(Equal(corrections.JobRelease))
		return p
	}
	undo := func(job *model.EditJob) *model.EditJob {
		GinkgoHelper()
		steps, err := editjob.UndoJobSteps(ctx, ds, job.ID)
		Expect(err).ToNot(HaveOccurred())
		u, err := q.EnqueueWith(ctx, model.EditUndo, editjob.UndoJobArgs(job.ID), steps)
		Expect(err).ToNot(HaveOccurred())
		return run(u)
	}

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-id", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"correction", "media_file", "folder", "album", "library_artist", "artist",
			"edit_journal", "edit_job_step", "edit_job", "annotation"} {
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
		al = model.Album{ID: "al-1", Name: "Recrod", AlbumArtist: "Band", MaxYear: 1999, LibraryID: 1, FolderIDs: []string{folder.ID}}
		Expect(ds.Album(ctx).Put(&al)).To(Succeed())

		tags := func(title, track string) map[string][]string {
			return map[string][]string{"TITLE": {title}, "ARTIST": {"Band"}, "ALBUMARTIST": {"Band"}, "ALBUM": {"Recrod"},
				"DATE": {"1999"}, "TRACKNUMBER": {track}, "GENRE": {"Alt Rock"}}
		}
		// One pairs by title and position; Two by title, and holds the ID of another recording
		// already (an earlier retag); Bonus Jam pairs with nothing.
		addFile("mf-1", "01 One.mp3", tags("One", "1"), model.MediaFile{Title: "One", Album: "Recrod", AlbumArtist: "Band", TrackNumber: 1})
		two := tags("Two (Remastered)", "2")
		two["MUSICBRAINZ_TRACKID"] = []string{otherRec}
		addFile("mf-2", "02 Two.mp3", two, model.MediaFile{Title: "Two (Remastered)", Album: "Recrod", AlbumArtist: "Band",
			TrackNumber: 2, MbzRecordingID: otherRec})
		addFile("mf-3", "09 Bonus Jam.mp3", tags("Bonus Jam", "9"), model.MediaFile{Title: "Bonus Jam", Album: "Recrod",
			AlbumArtist: "Band", TrackNumber: 9})

		svc = corrections.NewWithOptions(ds, corrections.Options{})
		editor = tagedit.New(ds, statScanner{ds: ds, root: libDir})
		q = editjob.New(ds, nil, editjob.Kinds(ds, editor))
		for kind, k := range corrections.JobKinds(ds) {
			q.Handle(kind, k)
		}
		q.Handle(corrections.JobRelease, corrections.ReleaseJobKind(ds, q))
	})

	Describe("the pairing", func() {
		It("pairs the album's Tracks with the release's tracks, listing those it can't pair and the IDs that differ", func() {
			m, err := svc.MatchRelease(ctx, al.ID, testRelease())
			Expect(err).ToNot(HaveOccurred())
			// Bonus Jam is one titled Track of three that pairs with nothing: too many for the
			// pairing to hold up, which the dialog says.
			Expect(m.Holds).To(BeFalse())
			Expect(m.Release.ID).To(Equal(relID))
			Expect(m.Release.ReleaseGroupID).To(Equal(groupID))
			Expect(m.Tracks).To(HaveLen(3))

			byID := map[string]corrections.PairedTrack{}
			for _, t := range m.Tracks {
				byID[t.ID] = t
			}
			Expect(byID["mf-1"].Paired).To(PointTo(MatchFields(IgnoreExtras, Fields{
				"ID": Equal(rt1), "RecordingID": Equal(rec1), "Position": Equal(1), "Disc": Equal(1)})))
			Expect(byID["mf-1"].Mismatch).To(BeFalse())
			Expect(byID["mf-2"].Paired).To(PointTo(HaveField("RecordingID", rec2)))
			Expect(byID["mf-2"].Mismatch).To(BeTrue(), "its File holds another recording's ID")
			Expect(byID["mf-2"].MbzRecordingID).To(Equal(otherRec))
			Expect(byID["mf-3"].Paired).To(BeNil(), "a Track that pairs with nothing is listed")
			Expect(m.Unpaired).To(ConsistOf(HaveField("ID", rt3)), "the release track no Track took")
			Expect(m.AlbumArtistMatches).To(BeTrue())

			Expect(m.Canonical.Album).To(Equal(corrections.CanonicalChange{From: "Recrod", To: "Record"}))
			Expect(m.Canonical.Date).To(Equal(corrections.CanonicalChange{From: "1999", To: "1999-03-02"}))
			Expect(m.Canonical.AlbumArtist).To(BeZero(), "the album artist is the release's already")
			Expect(m.Canonical.Titles).To(Equal(0), "Two's title is the release's already")
			Expect(m.Genres).To(Equal([]string{"Alternative Rock", "Rock"}), "the release group's genres")
		})

		It("pairs the Tracks no title pairs by track number when the album and the release have as many tracks", func() {
			// Bonus Jam is track 3 now, so the album's three Tracks are numbered as the release's.
			mf, err := ds.MediaFile(ctx).Get("mf-3")
			Expect(err).ToNot(HaveOccurred())
			mf.TrackNumber = 3
			Expect(ds.MediaFile(ctx).Put(mf)).To(Succeed())
			rel := testRelease()
			for i, title := range []string{"One", "Zwei", "Drei"} {
				rel.Media[0].Tracks[i].Title = title
			}

			m, err := svc.MatchRelease(ctx, al.ID, rel)
			Expect(err).ToNot(HaveOccurred())
			Expect(m.Holds).To(BeTrue(), "every Track pairs")
			Expect(m.ByPosition).To(Equal(2))
			byID := map[string]corrections.PairedTrack{}
			for _, t := range m.Tracks {
				byID[t.ID] = t
			}
			Expect(byID["mf-1"].ByTitle).To(BeTrue())
			Expect(byID["mf-1"].ByPosition).To(BeFalse())
			Expect(byID["mf-2"].Paired).To(PointTo(HaveField("ID", rt2)))
			Expect(byID["mf-2"].ByPosition).To(BeTrue())
			Expect(byID["mf-3"].Paired).To(PointTo(HaveField("ID", rt3)))
			Expect(m.Unpaired).To(BeEmpty())
			Expect(m.Canonical.Titles).To(Equal(2))

			p, err := svc.PlanRelease(ctx, editor, al.ID, rel, corrections.ReleaseWrite{Accept: corrections.Canonical{Titles: true}})
			Expect(err).ToNot(HaveOccurred())
			rw := rewrites(p)
			Expect(rw["mf-2"].Set).To(HaveKeyWithValue("title", []string{"Zwei"}))
			Expect(rw["mf-3"].Set).To(HaveKeyWithValue("title", []string{"Drei"}))
			Expect(rw["mf-3"].Fill).To(HaveKeyWithValue("musicbrainz_trackid", []string{rec3}))

			// One more track on the release: the counts differ, so nothing pairs by number.
			rel.Media[0].Tracks = append(rel.Media[0].Tracks, musicbrainz.Track{ID: "rt-extra", Position: 4, Title: "Vier"})
			rel.Media[0].TrackCount = 4
			m, err = svc.MatchRelease(ctx, al.ID, rel)
			Expect(err).ToNot(HaveOccurred())
			Expect(m.ByPosition).To(BeZero())
			Expect(m.Holds).To(BeFalse())
		})

		It("holds up when every Track pairs", func() {
			Expect(ds.MediaFile(ctx).Delete("mf-3")).To(Succeed())
			m, err := svc.MatchRelease(ctx, al.ID, testRelease())
			Expect(err).ToNot(HaveOccurred())
			Expect(m.Holds).To(BeTrue())
		})

		It("pairs nothing with a release with fewer tracks than the album, and says it doesn't hold", func() {
			rel := testRelease()
			rel.Media[0].Tracks, rel.Media[0].TrackCount = rel.Media[0].Tracks[:2], 2
			m, err := svc.MatchRelease(ctx, al.ID, rel)
			Expect(err).ToNot(HaveOccurred())
			Expect(m.Holds).To(BeFalse())
			for _, t := range m.Tracks {
				Expect(t.Paired).To(BeNil())
			}
		})

		It("scores each release by the Tracks that pair and how close their lengths are, marking the best fit", func() {
			// Bonus Jam is track 3 now, so it pairs by its number; the Files' durations are known.
			for id, d := range map[string]any{"mf-1": 200.5, "mf-2": 181.0, "mf-3": 240.0} {
				_, err := db.Db().Exec("update media_file set duration = ? where id = ?", d, id)
				Expect(err).ToNot(HaveOccurred())
			}
			_, err := db.Db().Exec("update media_file set track_number = 3 where id = 'mf-3'")
			Expect(err).ToNot(HaveOccurred())
			edition := func(id, status string, lengths ...int) *musicbrainz.Release {
				rel := testRelease()
				rel.ID, rel.Status = id, status
				for i := range rel.Media[0].Tracks {
					rel.Media[0].Tracks[i].Length = lengths[i]
				}
				return rel
			}
			// The remaster's lengths are 1-2s off the Files'; the original's within a second.
			remaster := edition("00000000-0000-4000-8000-00000000000a", "Official", 202000, 182000, 242000)
			original := edition(relID, "Promotion", 200000, 180000, 240000)
			short := edition("00000000-0000-4000-8000-00000000000b", "Official", 200000, 180000, 240000)
			short.Media[0].Tracks, short.Media[0].TrackCount = short.Media[0].Tracks[:2], 2
			noGroup := edition("00000000-0000-4000-8000-00000000000c", "Official", 200000, 180000, 240000)
			noGroup.ReleaseGroup.ID = ""

			fits, err := svc.FitReleases(ctx, al.ID, []*musicbrainz.Release{remaster, original, short, noGroup})
			Expect(err).ToNot(HaveOccurred())
			Expect(fits).To(HaveLen(3), "a release without its IDs is left unscored")
			Expect(fits[0]).To(MatchFields(IgnoreExtras, Fields{"ID": Equal(remaster.ID), "Paired": Equal(3),
				"TrackCount": Equal(3), "AlbumTracks": Equal(3), "LengthDiff": PointTo(Equal(1.5)), "Best": BeFalse()}))
			Expect(fits[1]).To(MatchFields(IgnoreExtras, Fields{"ID": Equal(relID), "Paired": Equal(3),
				"LengthDiff": PointTo(Equal(0.5)), "Best": BeTrue()}))
			Expect(fits[2]).To(MatchFields(IgnoreExtras, Fields{"Paired": Equal(0), "TrackCount": Equal(2),
				"LengthDiff": BeNil(), "Best": BeFalse()}), "a release with fewer tracks than the album pairs none")

			_, err = svc.FitReleases(ctx, "no-such-album", []*musicbrainz.Release{original})
			Expect(err).To(MatchError(model.ErrNotFound))
		})
	})

	Describe("the write plan", func() {
		It("writes the album's IDs into every File and the track IDs into the paired ones, where they hold none", func() {
			p := plan(corrections.ReleaseWrite{})
			rw := rewrites(p)
			// The album artist's ID goes with the credit's names, by position.
			album := tagedit.Values{"musicbrainz_albumid": {relID}, "musicbrainz_releasegroupid": {groupID},
				"musicbrainz_albumartistid": {bandID}, "albumartists": {"Band"}}
			Expect(rw).To(HaveLen(3))
			Expect(rw["mf-1"].Set).To(Equal(album))
			Expect(rw["mf-1"].Fill).To(Equal(tagedit.Values{"musicbrainz_trackid": {rec1}, "musicbrainz_releasetrackid": {rt1}}))
			// Fill never replaces the recording ID Two holds.
			Expect(rw["mf-2"].Set).To(Equal(album))
			Expect(rw["mf-2"].Fill).To(Equal(tagedit.Values{"musicbrainz_trackid": {rec2}, "musicbrainz_releasetrackid": {rt2}}))
			Expect(rw["mf-3"].Set).To(Equal(album))
			Expect(rw["mf-3"].Fill).To(BeEmpty(), "an unpaired Track gets no track IDs")
		})

		It("replaces the track IDs that differ only if the admin says so", func() {
			rw := rewrites(plan(corrections.ReleaseWrite{ReplaceTrackIDs: true}))
			Expect(rw["mf-2"].Set).To(HaveKeyWithValue("musicbrainz_trackid", []string{rec2}))
			Expect(rw["mf-2"].Set).To(HaveKeyWithValue("musicbrainz_releasetrackid", []string{rt2}))
			Expect(rw["mf-2"].Fill).To(BeEmpty())
		})

		It("writes the release's title, album artist, date and track titles only if the admin accepts them", func() {
			rw := rewrites(plan(corrections.ReleaseWrite{Accept: corrections.Canonical{Album: true, AlbumArtist: true,
				Date: true, Titles: true}}))
			Expect(rw["mf-1"].Set).To(And(
				HaveKeyWithValue("album", []string{"Record"}),
				HaveKeyWithValue("albumartists", []string{"Band"}),
				HaveKeyWithValue("musicbrainz_albumartistid", []string{bandID}),
				HaveKeyWithValue("date", []string{"1999-03-02"}),
				HaveKeyWithValue("originaldate", []string{"1998-11-01"}),
				HaveKeyWithValue("title", []string{"One"}),
			))
			Expect(rw["mf-1"].Set).ToNot(HaveKey("albumartist"), "the album artist is the release's already")
			Expect(rw["mf-3"].Set).ToNot(HaveKey("title"), "an unpaired Track keeps its title")

			rw = rewrites(plan(corrections.ReleaseWrite{}))
			Expect(rw["mf-1"].Set).ToNot(HaveKey("album"))
			Expect(rw["mf-1"].Set).ToNot(HaveKey("title"))
			Expect(rw["mf-1"].Set).ToNot(HaveKey("date"))
		})

		It("writes the dialog's own values and Track numbers in the same job", func() {
			rw := rewrites(plan(corrections.ReleaseWrite{Fix: &corrections.AlbumFix{Name: "Record!", AlbumArtist: "Band", Year: 1999},
				Tracks: []corrections.TrackNumber{{ID: "mf-3", TrackNumber: 4}}}))
			Expect(rw["mf-1"].Set).To(HaveKeyWithValue("album", []string{"Record!"}))
			Expect(rw["mf-3"].Set).To(HaveKeyWithValue("tracknumber", []string{"4"}))
		})

		It("leaves the album artist's MusicBrainz ID out when the album artist isn't the release's", func() {
			_, err := db.Db().Exec("update album set album_artist = 'Someone Else' where id = 'al-1'")
			Expect(err).ToNot(HaveOccurred())
			m, err := svc.MatchRelease(ctx, al.ID, testRelease())
			Expect(err).ToNot(HaveOccurred())
			Expect(m.AlbumArtistMatches).To(BeFalse())
			rw := rewrites(plan(corrections.ReleaseWrite{}))
			Expect(rw["mf-1"].Set).ToNot(HaveKey("musicbrainz_albumartistid"))
			Expect(rw["mf-1"].Set).ToNot(HaveKey("albumartists"))
		})

		It("writes the release's cover as the album's cover.jpg first, if the admin asks for it", func() {
			cover, err := os.ReadFile(filepath.Join("tests", "fixtures", "artist", "an-album", "cover.jpg"))
			Expect(err).ToNot(HaveOccurred())
			stored, err := artwork.StoreChosenCover(cover)
			Expect(err).ToNot(HaveOccurred())
			covers := &fakePickedCovers{covers: map[string]artwork.StoredCover{relID: stored}}
			svc = corrections.NewWithOptions(ds, corrections.Options{PickedCover: covers.fetch})

			p := plan(corrections.ReleaseWrite{Cover: true})
			Expect(p.PickCover).To(Equal(corrections.PickCoverFound))
			Expect(p.Steps[0].Kind).To(Equal(model.EditCover))
			Expect(p.Steps).To(HaveLen(4))

			run(enqueue(p))
			written, err := os.ReadFile(filepath.Join(dir, "cover.jpg"))
			Expect(err).ToNot(HaveOccurred())
			Expect(written).To(Equal(cover))

			// An image that can't be made a cover: the IDs are written all the same.
			bad := artwork.StoredCover{Hash: "0123456789abcdef", Mime: "image/jpeg"}
			Expect(artwork.ChosenCoverStore().Write(bad.Hash, bad.Mime, strings.NewReader("not an image"))).To(Succeed())
			covers.covers = map[string]artwork.StoredCover{relID: bad}
			p = plan(corrections.ReleaseWrite{Cover: true})
			Expect(p.PickCover).To(Equal(corrections.PickCoverFailed))
			Expect(p.Steps).To(HaveLen(3))

			// No cover in the archive: the IDs are written all the same.
			covers.covers = nil
			p = plan(corrections.ReleaseWrite{Cover: true})
			Expect(p.PickCover).To(Equal(corrections.PickCoverNone))
			Expect(p.Steps).To(HaveLen(3))
		})

		It("writes the release without its cover when the album has no folder of its own for one", func() {
			// Another album's Track in the album's folder: a cover.jpg there would be its cover too
			// (as with an album's disc folders under an artist folder holding other albums).
			addFile("mf-other", "99 Elsewhere.mp3", map[string][]string{"TITLE": {"Elsewhere"}, "ALBUM": {"Other"}},
				model.MediaFile{Title: "Elsewhere", Album: "Other", AlbumArtist: "Band"})
			_, err := db.Db().Exec("update media_file set album_id = 'al-other' where id = 'mf-other'")
			Expect(err).ToNot(HaveOccurred())
			covers := &fakePickedCovers{}
			svc = corrections.NewWithOptions(ds, corrections.Options{PickedCover: covers.fetch})

			p := plan(corrections.ReleaseWrite{Cover: true})
			Expect(p.PickCover).To(Equal(corrections.PickCoverNoFolder))
			Expect(covers.asked).To(BeEmpty(), "the archive isn't asked for a cover that can't be written")
			for _, s := range p.Steps {
				Expect(s.Kind).To(Equal(model.EditRewrite))
			}
			job := run(enqueue(p))
			Expect(job.Skipped).To(BeZero())
			Expect(tagsOf("01 One.mp3")).To(HaveKeyWithValue("MUSICBRAINZ_ALBUMID", []string{relID}))
		})
	})

	Describe("the job", func() {
		It("writes the album's genres into every File only if the admin asks, and its undo puts the old ones back", func() {
			rw := rewrites(plan(corrections.ReleaseWrite{}))
			Expect(rw["mf-1"].Set).ToNot(HaveKey("genre"))

			job := run(enqueue(plan(corrections.ReleaseWrite{Genres: true})))
			for _, name := range []string{"01 One.mp3", "02 Two.mp3", "09 Bonus Jam.mp3"} {
				Expect(tagsOf(name)).To(HaveKeyWithValue("GENRE", []string{"Alternative Rock", "Rock"}))
			}

			undo(job)
			Expect(tagsOf("01 One.mp3")).To(HaveKeyWithValue("GENRE", []string{"Alt Rock"}))
		})

		It("leaves the Files' genres alone when MusicBrainz has none for the album", func() {
			rel := testRelease()
			rel.Genres, rel.ReleaseGroup.Genres = nil, nil
			m, err := svc.MatchRelease(ctx, al.ID, rel)
			Expect(err).ToNot(HaveOccurred())
			Expect(m.Genres).To(BeEmpty())
			p, err := svc.PlanRelease(ctx, editor, al.ID, rel, corrections.ReleaseWrite{Genres: true})
			Expect(err).ToNot(HaveOccurred())
			Expect(rewrites(p)["mf-1"].Set).ToNot(HaveKey("genre"))
		})

		It("writes the IDs into the Files as one job, never replacing a track ID silently, and its undo takes them out", func() {
			job := run(enqueue(plan(corrections.ReleaseWrite{})))
			Expect(job.Kind).To(Equal(corrections.JobRelease))
			Expect(job.Skipped).To(BeZero())

			one, two, bonus := tagsOf("01 One.mp3"), tagsOf("02 Two.mp3"), tagsOf("09 Bonus Jam.mp3")
			for _, tags := range []map[string][]string{one, two, bonus} {
				Expect(tags).To(HaveKeyWithValue("MUSICBRAINZ_ALBUMID", []string{relID}))
				Expect(tags).To(HaveKeyWithValue("MUSICBRAINZ_RELEASEGROUPID", []string{groupID}))
				Expect(tags).To(HaveKeyWithValue("MUSICBRAINZ_ALBUMARTISTID", []string{bandID}))
				Expect(tags).To(HaveKeyWithValue("ALBUM", []string{"Recrod"}), "the title is the admin's to accept")
			}
			Expect(one).To(HaveKeyWithValue("MUSICBRAINZ_TRACKID", []string{rec1}))
			Expect(one).To(HaveKeyWithValue("MUSICBRAINZ_RELEASETRACKID", []string{rt1}))
			Expect(two).To(HaveKeyWithValue("MUSICBRAINZ_TRACKID", []string{otherRec}))
			Expect(two).To(HaveKeyWithValue("MUSICBRAINZ_RELEASETRACKID", []string{rt2}))
			Expect(bonus).ToNot(HaveKey("MUSICBRAINZ_TRACKID"))

			undo(job)
			one, two = tagsOf("01 One.mp3"), tagsOf("02 Two.mp3")
			Expect(one).ToNot(HaveKey("MUSICBRAINZ_ALBUMID"))
			Expect(one).ToNot(HaveKey("MUSICBRAINZ_TRACKID"))
			Expect(two).To(HaveKeyWithValue("MUSICBRAINZ_TRACKID", []string{otherRec}))
			Expect(two).ToNot(HaveKey("MUSICBRAINZ_RELEASETRACKID"))
		})

		It("puts every written File back when one couldn't be written, so the album isn't split", func() {
			job := enqueue(plan(corrections.ReleaseWrite{Accept: corrections.Canonical{Album: true}}))
			// Two gets a second name after the plan: the write path refuses it.
			other := filepath.Join(libDir, "elsewhere.mp3")
			Expect(os.Link(filepath.Join(dir, "02 Two.mp3"), other)).To(Succeed())
			job = run(job)
			Expect(job.Skipped).To(Equal(1))

			rollback, err := corrections.ReleaseRollback(job)
			Expect(err).ToNot(HaveOccurred())
			Expect(rollback).ToNot(BeEmpty(), "the job queued its rollback")
			// Run again (the job's row wasn't saved after it), Done finds that rollback rather than
			// queueing another.
			again := *job
			again.Args = `{"release":true}`
			Expect(corrections.ReleaseJobKind(ds, q).Done(ctx, &again, job.Steps)).To(Succeed())
			Expect(corrections.ReleaseRollback(&again)).To(Equal(rollback))
			run(&model.EditJob{ID: rollback})
			for _, name := range []string{"01 One.mp3", "02 Two.mp3", "09 Bonus Jam.mp3"} {
				tags := tagsOf(name)
				Expect(tags).ToNot(HaveKey("MUSICBRAINZ_ALBUMID"), name)
				Expect(tags).To(HaveKeyWithValue("ALBUM", []string{"Recrod"}), name)
			}

			// Once the other name is gone, saving again writes them all.
			Expect(os.Remove(other)).To(Succeed())
			job = run(enqueue(plan(corrections.ReleaseWrite{})))
			Expect(job.Skipped).To(BeZero())
			Expect(corrections.ReleaseRollback(job)).To(BeEmpty())
			Expect(tagsOf("02 Two.mp3")).To(HaveKeyWithValue("MUSICBRAINZ_ALBUMID", []string{relID}))
		})

		It("takes the release off a Correction not yet migrated, so the migration doesn't write the old one, and its undo puts it back", func() {
			cr := model.Correction{Kind: model.CorrectionAlbum, MatchKey: folder.ID, From: "Recrod", To: "MusicBrainz: Old",
				MbzAlbumID: "a7f8ed5e-1111-4a1b-9c1d-123456789abc", MbzReleaseGroupID: "b7f8ed5e-1111-4a1b-9c1d-123456789abc"}
			Expect(ds.Correction(ctx).Add(&cr)).To(Succeed())
			job := run(enqueue(plan(corrections.ReleaseWrite{})))
			Expect(ds.Correction(ctx).GetAll()).To(BeEmpty())

			undo(job)
			left, err := ds.Correction(ctx).GetAll()
			Expect(err).ToNot(HaveOccurred())
			Expect(left).To(ConsistOf(HaveField("MbzAlbumID", "a7f8ed5e-1111-4a1b-9c1d-123456789abc")))
		})
	})
})
