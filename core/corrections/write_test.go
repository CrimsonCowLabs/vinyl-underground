package corrections_test

import (
	"context"
	"encoding/json"

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
)

// rewrites returns each step's Track and the Rewrite it writes.
func rewrites(plan *corrections.Plan) map[string]editjob.Rewrite {
	GinkgoHelper()
	res := map[string]editjob.Rewrite{}
	for _, s := range plan.Steps {
		Expect(s.Kind).To(Equal(model.EditRewrite))
		var r editjob.Rewrite
		Expect(json.Unmarshal([]byte(s.Args), &r)).To(Succeed())
		res[s.MediaFileID] = r
	}
	return res
}

var _ = Describe("Writing Corrections to the Files", func() {
	var (
		ctx   context.Context
		ds    model.DataStore
		svc   corrections.Corrections
		kinds map[model.EditKind]editjob.JobKind
		al    model.Album
		cd1   *model.Folder
		cd2   *model.Folder
	)

	numberTrack := func(id string, disc, track int) {
		GinkgoHelper()
		_, err := db.Db().Exec("update media_file set disc_number = ?, track_number = ? where id = ?", disc, track, id)
		Expect(err).ToNot(HaveOccurred())
	}
	// queued keeps the plan's job, as the queue would, and returns it with its steps ended as
	// states says (by Track; written when not said).
	queued := func(plan *corrections.Plan, states map[string]model.EditStepState) (*model.EditJob, model.EditJobSteps) {
		GinkgoHelper()
		job := &model.EditJob{Kind: plan.Kind, Args: plan.Args, State: model.EditJobRunning, Total: len(plan.Steps)}
		Expect(ds.EditJob(ctx).Put(job)).To(Succeed())
		var steps model.EditJobSteps
		for _, s := range plan.Steps {
			s.JobID, s.State = job.ID, model.EditStepWritten
			if st, ok := states[s.MediaFileID]; ok {
				s.State = st
			}
			steps = append(steps, s)
		}
		return job, steps
	}
	correctionsOf := func(kind model.CorrectionKind) model.Corrections {
		GinkgoHelper()
		all, err := ds.Correction(ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		var res model.Corrections
		for _, c := range all {
			if c.Kind == kind {
				res = append(res, c)
			}
		}
		return res
	}

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-id", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"correction", "media_file", "folder", "album", "library_artist", "artist", "edit_job"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		svc = corrections.NewWithOptions(ds, corrections.Options{})
		kinds = corrections.JobKinds(ds)

		cd1 = model.NewFolder(model.Library{ID: 1}, "Band/Record/CD1")
		cd2 = model.NewFolder(model.Library{ID: 1}, "Band/Record/CD2")
		for _, f := range []*model.Folder{cd1, cd2} {
			Expect(ds.Folder(ctx).Put(f)).To(Succeed())
		}
		al = model.Album{ID: "al-1", Name: "Recrod", AlbumArtist: "Band", MaxYear: 1998, LibraryID: 1,
			FolderIDs: []string{cd1.ID, cd2.ID}}
		Expect(ds.Album(ctx).Put(&al)).To(Succeed())
		addTrack("mf-1", al.ID, cd1)
		addTrack("mf-2", al.ID, cd2)
		numberTrack("mf-1", 1, 1)
		numberTrack("mf-2", 2, 1)
	})

	Describe("the album Edit dialog", func() {
		fix := func() *corrections.AlbumFix {
			return &corrections.AlbumFix{Name: " Record ", AlbumArtist: "The  Band", Year: 1999}
		}

		It("writes the title, album artist and year into every File of the album, as one job", func() {
			plan, err := svc.PlanAlbumEdit(ctx, al.ID, fix(), nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(plan.Kind).To(Equal(corrections.JobAlbumEdit))
			Expect(plan.TrackID).ToNot(BeEmpty())
			want := editjob.Rewrite{
				Set:  tagedit.Values{"album": {"Record"}, "albumartist": {"The Band"}},
				Drop: map[string][]string{"albumartist": {"albumartists", "musicbrainz_albumartistid"}},
				Year: 1999,
			}
			Expect(rewrites(plan)).To(Equal(map[string]editjob.Rewrite{"mf-1": want, "mf-2": want}))
			// No Correction is saved.
			Expect(ds.Correction(ctx).GetAll()).To(BeEmpty())
		})

		It("writes only what changed, and the Tracks' new numbers with it", func() {
			plan, err := svc.PlanAlbumEdit(ctx, al.ID, &corrections.AlbumFix{Name: "Record", AlbumArtist: "Band"},
				[]corrections.TrackNumber{{ID: "mf-2", DiscNumber: 2, TrackNumber: 5}})
			Expect(err).ToNot(HaveOccurred())
			Expect(rewrites(plan)).To(Equal(map[string]editjob.Rewrite{
				"mf-1": {Set: tagedit.Values{"album": {"Record"}}},
				"mf-2": {Set: tagedit.Values{"album": {"Record"}, "tracknumber": {"5"}, "discnumber": {"2"}}},
			}))
		})

		It("writes the composers into every File when given, and leaves each File's own otherwise", func() {
			plan, err := svc.PlanAlbumEdit(ctx, al.ID, &corrections.AlbumFix{Name: "Recrod", AlbumArtist: "Band",
				Composer: []string{" Barry White ", "", "Valerie Simpson"}}, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(plan.Kind).To(Equal(corrections.JobAlbumEdit))
			want := editjob.Rewrite{Set: tagedit.Values{"composer": {"Barry White", "Valerie Simpson"}}}
			Expect(rewrites(plan)).To(Equal(map[string]editjob.Rewrite{"mf-1": want, "mf-2": want}))

			_, err = svc.PlanAlbumEdit(ctx, al.ID, &corrections.AlbumFix{Name: "Recrod", AlbumArtist: "Band",
				Composer: []string{" "}}, nil)
			Expect(err).To(MatchError(corrections.ErrNothingToWrite), "blank composers keep each File's own")
		})

		It("writes Track order alone as a job of its own kind", func() {
			plan, err := svc.PlanAlbumEdit(ctx, al.ID, nil, []corrections.TrackNumber{{ID: "mf-1", DiscNumber: 1, TrackNumber: 2}})
			Expect(err).ToNot(HaveOccurred())
			Expect(plan.Kind).To(Equal(corrections.JobTrackNumbers))
			Expect(rewrites(plan)).To(Equal(map[string]editjob.Rewrite{
				"mf-1": {Set: tagedit.Values{"tracknumber": {"2"}, "discnumber": {"1"}}},
			}))
		})

		It("refuses a save that changes nothing, and numbers that are no numbers", func() {
			_, err := svc.PlanAlbumEdit(ctx, al.ID, &corrections.AlbumFix{Name: "Recrod", AlbumArtist: "Band", Year: 1998}, nil)
			Expect(err).To(MatchError(corrections.ErrNothingToWrite))
			_, err = svc.PlanAlbumEdit(ctx, al.ID, nil, []corrections.TrackNumber{{ID: "mf-1", DiscNumber: 0, TrackNumber: 2}})
			Expect(err).To(MatchError(model.ErrValidation))
			_, err = svc.PlanAlbumEdit(ctx, al.ID, nil, []corrections.TrackNumber{{ID: "other", DiscNumber: 1, TrackNumber: 2}})
			Expect(err).To(MatchError(model.ErrValidation))
		})

		It("writes what an album Correction overrides, and removes that from it once the Files are written", func() {
			old := model.Correction{Kind: model.CorrectionAlbum, MatchKey: model.AlbumFolderKey(al), From: "Band – Recrod",
				To: "Band – Recrod (1997)", Year: 1997, MbzAlbumID: "a7f8ed5e-1111-4a1b-9c1d-123456789abc",
				MbzReleaseGroupID: "b7f8ed5e-1111-4a1b-9c1d-123456789abc"}
			Expect(ds.Correction(ctx).Add(&old)).To(Succeed())
			al.MaxYear = 1997 // as the album shows it
			Expect(ds.Album(ctx).Put(&al)).To(Succeed())

			plan, err := svc.PlanAlbumEdit(ctx, al.ID, &corrections.AlbumFix{Name: "Recrod", AlbumArtist: "Band", Year: 1997}, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(rewrites(plan)["mf-1"].Year).To(Equal(1997))

			job, steps := queued(plan, nil)
			Expect(kinds[plan.Kind].Done(ctx, job, steps)).To(Succeed())
			left := correctionsOf(model.CorrectionAlbum)
			Expect(left).To(HaveLen(1))
			// The MusicBrainz release stays, for the Correction migration to write; the year is the
			// Files' now.
			Expect(left[0].Year).To(BeZero())
			Expect(left[0].MbzAlbumID).To(Equal(old.MbzAlbumID))

			// Running again (a restart) changes nothing more, and the undo puts it back whole.
			Expect(kinds[plan.Kind].Done(ctx, job, steps)).To(Succeed())
			Expect(kinds[plan.Kind].Undone(ctx, job, steps)).To(Succeed())
			left = correctionsOf(model.CorrectionAlbum)
			Expect(left).To(HaveLen(1))
			Expect(left[0].Year).To(Equal(1997))
			Expect(left[0].MbzAlbumID).To(Equal(old.MbzAlbumID))
		})

		It("deletes an album Correction it leaves with nothing, and removes only the written Files' track numbers", func() {
			old := model.Correction{Kind: model.CorrectionAlbum, MatchKey: model.AlbumFolderKey(al), AlbumName: "Recrod"}
			Expect(ds.Correction(ctx).Add(&old)).To(Succeed())
			nums := model.Correction{Kind: model.CorrectionTrackNumbers, MatchKey: model.AlbumFolderKey(al), TrackNumbers: model.TrackNumberFixes{
				{LibraryID: 1, Path: "Band/Record/CD1/mf-1.mp3", Title: "One", FromDisc: 1, FromTrack: 3, Disc: 1, Track: 1},
				{LibraryID: 1, Path: "Band/Record/CD2/mf-2.mp3", Title: "Two", FromDisc: 2, FromTrack: 3, Disc: 2, Track: 1},
			}}
			Expect(ds.Correction(ctx).Add(&nums)).To(Succeed())

			plan, err := svc.PlanAlbumEdit(ctx, al.ID, &corrections.AlbumFix{Name: "Record", AlbumArtist: "Band"},
				[]corrections.TrackNumber{{ID: "mf-1", DiscNumber: 1, TrackNumber: 1}, {ID: "mf-2", DiscNumber: 2, TrackNumber: 1}})
			Expect(err).ToNot(HaveOccurred())
			job, steps := queued(plan, map[string]model.EditStepState{"mf-2": model.EditStepSkipped})
			Expect(kinds[plan.Kind].Done(ctx, job, steps)).To(Succeed())

			// The skipped File keeps its fixes: it still holds the old title and number. The album
			// Correction is the whole album's, so it stays whole.
			Expect(correctionsOf(model.CorrectionAlbum)).To(HaveLen(1))
			left := correctionsOf(model.CorrectionTrackNumbers)
			Expect(left).To(HaveLen(1))
			Expect(left[0].TrackNumbers).To(HaveLen(1))
			Expect(left[0].TrackNumbers[0].Path).To(Equal("Band/Record/CD2/mf-2.mp3"))

			// With every File written, an album Correction left with nothing is deleted.
			steps[1].State = model.EditStepWritten
			Expect(kinds[plan.Kind].Done(ctx, job, steps)).To(Succeed())
			Expect(correctionsOf(model.CorrectionAlbum)).To(BeEmpty())
		})

		It("leaves the Corrections alone when no File was written", func() {
			old := model.Correction{Kind: model.CorrectionAlbum, MatchKey: model.AlbumFolderKey(al), AlbumName: "Recrod"}
			Expect(ds.Correction(ctx).Add(&old)).To(Succeed())
			plan, err := svc.PlanAlbumEdit(ctx, al.ID, &corrections.AlbumFix{Name: "Record", AlbumArtist: "Band"}, nil)
			Expect(err).ToNot(HaveOccurred())
			job, steps := queued(plan, map[string]model.EditStepState{"mf-1": model.EditStepSkipped, "mf-2": model.EditStepSkipped})
			Expect(kinds[plan.Kind].Done(ctx, job, steps)).To(Succeed())
			Expect(correctionsOf(model.CorrectionAlbum)).To(HaveLen(1))
		})
	})

	Describe("artist rename and merge", func() {
		var dead model.Artist
		addArtist := func(name string) model.Artist {
			GinkgoHelper()
			ar := model.Artist{ID: metadata.ArtistID(name), Name: name}
			Expect(ds.Artist(ctx).Put(&ar)).To(Succeed())
			Expect(ds.Library(ctx).AddArtist(1, ar.ID)).To(Succeed())
			return ar
		}
		credit := func(trackID string, role model.Role, ar model.Artist) {
			GinkgoHelper()
			mf, err := ds.MediaFile(ctx).Get(trackID)
			Expect(err).ToNot(HaveOccurred())
			if mf.Participants == nil {
				mf.Participants = model.Participants{}
			}
			mf.Participants.Add(role, ar)
			Expect(ds.MediaFile(ctx).Put(mf)).To(Succeed())
		}

		BeforeEach(func() {
			dead = addArtist("Greatful Dead")
			credit("mf-1", model.RoleArtist, dead)
		})

		It("renames the Artist in every File that names it, by every name it has there, as one job", func() {
			// A Correction renames "Grateful Ded" to the Artist: its Files hold that name.
			typo := model.Correction{Kind: model.CorrectionArtistRename, MatchKey: "grateful ded", From: "Grateful Ded", To: "Greatful Dead"}
			Expect(ds.Correction(ctx).Add(&typo)).To(Succeed())
			credit("mf-2", model.RoleAlbumArtist, dead)

			plan, err := svc.PlanArtistRename(ctx, dead.ID, " Grateful  Dead")
			Expect(err).ToNot(HaveOccurred())
			Expect(plan.Kind).To(Equal(corrections.JobArtistRename))
			Expect(plan.ArtistID).To(Equal(metadata.ArtistID("Grateful Dead")))
			rename := editjob.Rewrite{Rename: &editjob.Rename{From: []string{"Greatful Dead", "Grateful Ded"}, To: "Grateful Dead"}}
			Expect(rewrites(plan)).To(Equal(map[string]editjob.Rewrite{"mf-1": rename, "mf-2": rename}))
			Expect(correctionsOf(model.CorrectionArtistRename)).To(HaveLen(1))
		})

		It("refuses what renaming refuses: another Artist's name (a merge), or a name a Correction renames away", func() {
			addArtist("Grateful Dead")
			_, err := svc.PlanArtistRename(ctx, dead.ID, "grateful dead")
			Expect(err).To(MatchError(corrections.ErrArtistExists))
			away := model.Correction{Kind: model.CorrectionArtistRename, MatchKey: "the dead", From: "The Dead", To: "Dead"}
			Expect(ds.Correction(ctx).Add(&away)).To(Succeed())
			_, err = svc.PlanArtistRename(ctx, dead.ID, "The Dead")
			Expect(err).To(MatchError(corrections.ErrRenamedAway))
		})

		It("gives the new name as album artist to the Tracks an album Correction gives the Artist", func() {
			albumFix := model.Correction{Kind: model.CorrectionAlbum, MatchKey: model.AlbumFolderKey(al), AlbumArtist: "Greatful Dead", Year: 1977}
			Expect(ds.Correction(ctx).Add(&albumFix)).To(Succeed())
			credit("mf-2", model.RoleAlbumArtist, dead)

			// The album Correction applies to every File in the album's folders, as it did on read.
			plan, err := svc.PlanArtistRename(ctx, dead.ID, "Grateful Dead")
			Expect(err).ToNot(HaveOccurred())
			Expect(rewrites(plan)["mf-2"].Set).To(Equal(tagedit.Values{"albumartist": {"Grateful Dead"}}))
			Expect(rewrites(plan)["mf-1"].Set).To(Equal(tagedit.Values{"albumartist": {"Grateful Dead"}}))

			job, steps := queued(plan, nil)
			Expect(kinds[plan.Kind].Done(ctx, job, steps)).To(Succeed())
			left := correctionsOf(model.CorrectionAlbum)
			Expect(left).To(HaveLen(1))
			Expect(left[0].AlbumArtist).To(BeEmpty())
			Expect(left[0].Year).To(Equal(1977))
		})

		It("removes the renames its Files no longer need once they are all written, and the undo puts them back", func() {
			typo := model.Correction{Kind: model.CorrectionArtistRename, MatchKey: "grateful ded", From: "Grateful Ded", To: "Greatful Dead"}
			Expect(ds.Correction(ctx).Add(&typo)).To(Succeed())
			// An older merge into the Artist keeps the stars it merged, for its own undo: it stays.
			merge := model.Correction{Kind: model.CorrectionArtistMerge, MatchKey: "the greatful dead", From: "The Greatful Dead", To: "Greatful Dead"}
			Expect(ds.Correction(ctx).Add(&merge)).To(Succeed())
			into := addArtist("Grateful Dead")
			plan, err := svc.PlanArtistMerge(ctx, dead.ID, into.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(plan.Kind).To(Equal(corrections.JobArtistMerge))
			Expect(plan.ArtistID).To(Equal(into.ID))
			Expect(rewrites(plan)["mf-1"].Rename.From).To(ConsistOf("Greatful Dead", "Grateful Ded", "The Greatful Dead"))

			k := kinds[plan.Kind]
			job, steps := queued(plan, map[string]model.EditStepState{"mf-1": model.EditStepSkipped})
			Expect(k.Start(ctx, job)).To(Succeed())
			// A File was skipped: it still holds the old name, and needs the rename.
			Expect(k.Done(ctx, job, steps)).To(Succeed())
			Expect(correctionsOf(model.CorrectionArtistRename)).To(HaveLen(1))

			steps[0].State = model.EditStepWritten
			Expect(k.Done(ctx, job, steps)).To(Succeed())
			Expect(correctionsOf(model.CorrectionArtistRename)).To(BeEmpty())
			Expect(correctionsOf(model.CorrectionArtistMerge)).To(HaveLen(1))

			Expect(k.Undone(ctx, job, steps)).To(Succeed())
			back := correctionsOf(model.CorrectionArtistRename)
			Expect(back).To(HaveLen(1))
			Expect(back[0].MatchKey).To(Equal("grateful ded"))
			Expect(back[0].To).To(Equal("Greatful Dead"))
		})
	})
})
