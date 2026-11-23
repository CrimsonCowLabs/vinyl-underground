package corrections_test

import (
	"context"
	"errors"

	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A MusicBrainz release picked with Find on MusicBrainz stays a Correction (app data) until its IDs
// are written into the Files (#170): the album shows its Cover Art Archive cover.
var _ = Describe("CorrectAlbum", func() {
	var ctx context.Context
	var ds model.DataStore
	var svc corrections.Corrections
	var al model.Album
	var cd1, cd2 *model.Folder

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-id", UserName: "admin", IsAdmin: true})
		ds = persistence.New(db.Db())
		for _, table := range []string{"correction", "media_file", "folder", "album", "library_artist", "artist"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		svc = corrections.NewWithOptions(ds, corrections.Options{})

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
	})

	key := func() string { return model.AlbumFolderKey(al) }

	It("rejects an album sharing a folder with another album, which the Correction would change too", func() {
		addTrack("other", "al-2", cd1)
		_, err := svc.CorrectAlbum(ctx, al.ID, corrections.AlbumFix{MbzAlbumID: "8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c",
			MbzReleaseGroupID: "1b2c3d4e-5f60-4718-9a0b-c1d2e3f40516"})
		Expect(err).To(MatchError(corrections.ErrSharedFolder))
	})

	Describe("MusicBrainz release (Find on MusicBrainz)", func() {
		const release, group = "8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c", "1b2c3d4e-5f60-4718-9a0b-c1d2e3f40516"
		picked := corrections.AlbumFix{Name: "Recrod", AlbumArtist: "Band", Year: 1998,
			MbzAlbumID: release, MbzReleaseGroupID: group, MbzRelease: "Record (GB, CD, 1999)"}
		var archive *fakePickedCovers

		BeforeEach(func() {
			archive = &fakePickedCovers{covers: map[string]artwork.StoredCover{
				release:                                {Hash: "aaaaaaaaaaaaaaaa", Mime: "image/png"},
				"00000000-0000-4000-8000-000000000001": {Hash: "bbbbbbbbbbbbbbbb", Mime: "image/jpeg"},
			}}
			svc = corrections.NewWithOptions(ds, corrections.Options{PickedCover: archive.fetch})
		})
		queued := func(kind model.Kind, id string) bool {
			_, err := ds.ArtworkQueue(ctx).Get(kind, id, model.ImageTypePrimary)
			return err == nil
		}

		It("saves the release and release group IDs alone, keyed by the album's folder, without rescanning", func() {
			res, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Done).To(BeTrue())
			Expect(res.AlbumID).To(Equal(al.ID), "the release isn't applied to the tags (#170)")
			Expect(res.Correction.From).To(Equal("Band – Recrod (1998)"))
			Expect(res.Correction.To).To(Equal("Band – Recrod (1998) · MusicBrainz: Record (GB, CD, 1999)"))

			got, err := ds.Correction(ctx).GetByMatchKey(model.CorrectionAlbum, key())
			Expect(err).ToNot(HaveOccurred())
			Expect(got.MbzAlbumID).To(Equal(release))
			Expect(got.MbzReleaseGroupID).To(Equal(group))
			Expect(got.AlbumName).To(BeEmpty())
			Expect(got.Year).To(BeZero())
		})

		It("keeps the release's Cover Art Archive cover with it, and refreshes the album's and its Tracks' art", func() {
			_, err := db.Db().Exec("update media_file set has_cover_art = true")
			Expect(err).ToNot(HaveOccurred())
			res, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.PickCover).To(Equal(corrections.PickCoverFound))
			Expect(archive.asked).To(Equal([]string{release + "|" + group}))
			Expect(res.Correction.ImageHash).To(Equal("aaaaaaaaaaaaaaaa"))
			Expect(res.Correction.ImageMime).To(Equal("image/png"))
			Eventually(func() bool { return queued(model.KindMediaFileArtwork, "mf-1") }).Should(BeTrue())
			Expect(queued(model.KindAlbumArtwork, al.ID)).To(BeTrue())
		})

		It("saves the pick without a cover when the archive has none, or can't be reached", func() {
			archive.covers = nil
			res, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.PickCover).To(Equal(corrections.PickCoverNone))
			Expect(res.Correction.ImageHash).To(BeEmpty())
			Expect(res.Correction.MbzAlbumID).To(Equal(release))

			archive.err = errors.New("the archive is down")
			res, err = svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.PickCover).To(Equal(corrections.PickCoverFailed))
		})

		It("tries the archive again when the same release is picked again and has no cover yet", func() {
			archive.err = errors.New("the archive is down")
			_, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			archive.err = nil
			res, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.PickCover).To(Equal(corrections.PickCoverFound))
			Expect(res.Correction.ImageHash).To(Equal("aaaaaaaaaaaaaaaa"))

			_, err = svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).To(MatchError(ContainSubstring("nothing changed")), "once it has its cover")
		})

		It("brings forward the cover of the release the album is already tagged with", func() {
			al.MbzAlbumID, al.MbzReleaseGroupID = release, group
			Expect(ds.Album(ctx).Put(&al)).To(Succeed())
			res, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Correction.ImageHash).To(Equal("aaaaaaaaaaaaaaaa"))
		})

		It("refreshes the covers again on undo", func() {
			res, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool { return queued(model.KindAlbumArtwork, al.ID) }).Should(BeTrue())
			_, err = db.Db().Exec("delete from artwork_queue")
			Expect(err).ToNot(HaveOccurred())

			_, err = svc.Undo(ctx, res.Correction.ID)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool { return queued(model.KindAlbumArtwork, al.ID) }).Should(BeTrue())
		})

		It("keeps what an album Correction not yet migrated corrects", func() {
			old := model.Correction{Kind: model.CorrectionAlbum, MatchKey: key(), From: "Band – Recrod (1998)",
				To: "Band – Record (1998)", AlbumName: "Record"}
			Expect(ds.Correction(ctx).Add(&old)).To(Succeed())
			res, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Correction.AlbumName).To(Equal("Record"))
			Expect(res.Correction.To).To(Equal("Band – Record (1998) · MusicBrainz: Record (GB, CD, 1999)"))
			Expect(res.Correction.MbzAlbumID).To(Equal(release))
		})

		It("never saves a title, album artist or year: those go to the Files", func() {
			fix := picked
			fix.Name, fix.AlbumArtist, fix.Year = "Other", "Someone", 2001
			res, err := svc.CorrectAlbum(ctx, al.ID, fix)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Correction.AlbumName).To(BeEmpty())
			Expect(res.Correction.AlbumArtist).To(BeEmpty())
			Expect(res.Correction.Year).To(BeZero())
		})

		It("replaces a release picked before", func() {
			_, err := svc.CorrectAlbum(ctx, al.ID, picked)
			Expect(err).ToNot(HaveOccurred())
			other := picked
			other.MbzAlbumID, other.MbzRelease = "00000000-0000-4000-8000-000000000001", "Record (US, 12\" Vinyl, 1999)"
			res, err := svc.CorrectAlbum(ctx, al.ID, other)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Correction.MbzAlbumID).To(Equal("00000000-0000-4000-8000-000000000001"))
			Expect(res.Correction.To).To(HaveSuffix(`MusicBrainz: Record (US, 12" Vinyl, 1999)`))
			Expect(res.Correction.ImageHash).To(Equal("bbbbbbbbbbbbbbbb"), "with its own cover")
		})

		It("rejects IDs that are no MusicBrainz IDs", func() {
			for _, bad := range []corrections.AlbumFix{
				{Name: "Recrod", AlbumArtist: "Band", MbzAlbumID: "not-an-id"},
				{Name: "Recrod", AlbumArtist: "Band", MbzAlbumID: release, MbzReleaseGroupID: "nope"},
				{Name: "Recrod", AlbumArtist: "Band", MbzReleaseGroupID: group},
				{Name: "Recrod", AlbumArtist: "Band", MbzAlbumID: release},
			} {
				_, err := svc.CorrectAlbum(ctx, al.ID, bad)
				Expect(err).To(MatchError(model.ErrValidation), "%+v", bad)
			}
		})
	})

})

func addTrack(id, albumID string, f *model.Folder) {
	GinkgoHelper()
	ctx := request.WithUser(context.Background(), model.User{ID: "admin-id", UserName: "admin", IsAdmin: true})
	mf := model.MediaFile{ID: id, LibraryID: 1, FolderID: f.ID, AlbumID: albumID, Path: f.Path + "/" + f.Name + "/" + id + ".mp3",
		Participants: model.Participants{}}
	Expect(persistence.New(db.Db()).MediaFile(ctx).Put(&mf)).To(Succeed())
}

// fakePickedCovers stands in for the Cover Art Archive: the stored cover of each release it has.
type fakePickedCovers struct {
	covers map[string]artwork.StoredCover
	err    error
	asked  []string
}

func (f *fakePickedCovers) fetch(_ context.Context, releaseID, releaseGroupID string) (artwork.StoredCover, error) {
	f.asked = append(f.asked, releaseID+"|"+releaseGroupID)
	if f.err != nil {
		return artwork.StoredCover{}, f.err
	}
	c, ok := f.covers[releaseID]
	if !ok {
		return artwork.StoredCover{}, musicbrainz.ErrNoCover
	}
	return c, nil
}
