package match_test

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/rip/match"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The matcher runs against a real database, so the Library's full-text search and its library
// access rules are the real ones.
var _ = Describe("Matcher", Ordered, func() {
	const inboxID = 2
	var (
		ds                 model.DataStore
		adminCtx, listener context.Context
		m                  *match.Matcher
		seq                int
	)

	BeforeAll(func() {
		// db.Db() is a process-wide singleton, so the whole suite shares one database.
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DbPath = filepath.Join(GinkgoT().TempDir(), "match.db") + "?_journal_mode=WAL"
		DeferCleanup(db.Init(context.Background()))
		ds = persistence.New(db.Db())

		inbox := model.Library{ID: inboxID, Name: "Inbox", Path: GinkgoT().TempDir()}
		Expect(ds.Library(context.Background()).Put(&inbox)).To(Succeed())

		admin := model.User{ID: "admin-id", UserName: "admin", Name: "Admin", NewPassword: "pw", IsAdmin: true}
		alice := model.User{ID: "alice-id", UserName: "alice", Name: "Alice", NewPassword: "pw"}
		for _, u := range []*model.User{&admin, &alice} {
			Expect(ds.User(context.Background()).Put(u)).To(Succeed())
		}
		// Alice, a Listener, sees the main Library but not the Inbox.
		Expect(ds.User(context.Background()).SetUserLibraries(alice.ID, []int{1})).To(Succeed())
		alice.Libraries = model.Libraries{{ID: 1}}
		adminCtx = request.WithUser(context.Background(), admin)
		listener = request.WithUser(context.Background(), alice)
		m = match.New(ds)
	})

	BeforeEach(func() {
		_, err := db.Db().Exec("delete from media_file")
		Expect(err).ToNot(HaveOccurred())
	})

	// addTrack puts a Track in the library libID and returns its ID.
	addTrack := func(libID int, artist, title, album string, sourceURL ...string) string {
		seq++
		mf := model.MediaFile{
			ID: fmt.Sprintf("mf-%04d", seq), LibraryID: libID, Path: fmt.Sprintf("a/%d.opus", seq),
			Title: title, Artist: artist, AlbumArtist: artist, Album: album, Tags: model.Tags{},
		}
		if len(sourceURL) > 0 {
			mf.Source, mf.SourceURL = "YouTube", sourceURL[0]
		}
		Expect(ds.MediaFile(adminCtx).Put(&mf)).To(Succeed())
		return mf.ID
	}

	ids := func(ms model.RipMatches, err error) []string {
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
		res := []string{}
		for _, mt := range ms {
			res = append(res, mt.ID)
		}
		return res
	}

	It("finds a Track by the video's artist and title, despite the YouTube noise", func() {
		want := addTrack(1, "Daft Punk", "One More Time", "Discovery")
		addTrack(1, "Daft Punk", "Aerodynamic", "Discovery")

		got, err := m.AlreadyHave(adminCtx, "Daft Punk", "Daft Punk - One More Time (Official Video) [HD]")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(ConsistOf(model.RipMatch{
			ID: want, Title: "One More Time", Artist: "Daft Punk", Album: "Discovery", Library: "Music Library",
		}))
	})

	It("doesn't match the same title by another artist, or another recording", func() {
		addTrack(1, "Depeche Mode", "Halo", "Violator")
		addTrack(1, "Queen", "Bohemian Rhapsody (Live Aid)", "Live Aid")

		Expect(m.AlreadyHave(adminCtx, "Beyoncé", "Halo")).To(BeEmpty())
		Expect(m.AlreadyHave(adminCtx, "Queen Official", "Queen - Bohemian Rhapsody (Official Video)")).To(BeEmpty())
	})

	It("finds an earlier Rip in the Inbox, with its own YouTube title, and says it is a Rip", func() {
		rip := addTrack(inboxID, "BeyonceVEVO", "Beyoncé - Halo [HD]", "", "https://www.youtube.com/watch?v=bnVUHWCynig")

		got, err := m.AlreadyHave(adminCtx, "Beyonce", "Halo (Official Music Video)")
		Expect(err).ToNot(HaveOccurred())
		Expect(ids(got, nil)).To(Equal([]string{rip}))
		Expect(got[0].Library).To(Equal("Inbox"))
		Expect(got[0].SourceURL).To(Equal("https://www.youtube.com/watch?v=bnVUHWCynig"))
	})

	It("matches a VEVO channel's name to the artist", func() {
		want := addTrack(1, "Taylor Swift", "Shake It Off", "1989")
		Expect(ids(m.AlreadyHave(adminCtx, "TaylorSwiftVEVO", "Shake It Off"))).To(Equal([]string{want}))
	})

	It("matches through accents, punctuation and featuring credits", func() {
		sigur := addTrack(1, "Sigur Rós", "Hoppípolla", "Takk...")
		lucky := addTrack(1, "Daft Punk feat. Pharrell Williams", "Get Lucky", "Random Access Memories")
		acdc := addTrack(1, "AC/DC", "Back in Black", "Back in Black")

		Expect(ids(m.AlreadyHave(adminCtx, "Sigur Ros", "Hoppipolla"))).To(Equal([]string{sigur}))
		Expect(ids(m.AlreadyHave(adminCtx, "Daft Punk - Topic", "Get Lucky (feat. Pharrell Williams & Nile Rodgers)"))).To(Equal([]string{lucky}))
		Expect(ids(m.AlreadyHave(adminCtx, "acdcVEVO", "AC/DC - Back In Black (Official Video)"))).To(Equal([]string{acdc}))
	})

	It("lists every Track that seems to be the same, in the Library and the Inbox", func() {
		a := addTrack(1, "Adele", "Hello", "25")
		b := addTrack(inboxID, "Adele", "Hello (Official Video)", "", "https://www.youtube.com/watch?v=YQHsXMglC9A")
		Expect(ids(m.AlreadyHave(adminCtx, "Adele", "Adele - Hello"))).To(ConsistOf(a, b))
	})

	It("only looks in the libraries the user can see", func() {
		addTrack(inboxID, "Adele", "Hello", "")
		mine := addTrack(1, "Daft Punk", "One More Time", "Discovery")

		Expect(m.AlreadyHave(listener, "Adele", "Hello")).To(BeEmpty())
		Expect(ids(m.AlreadyHave(listener, "Daft Punk", "One More Time"))).To(Equal([]string{mine}))
	})

	It("finds nothing for a video without a title", func() {
		addTrack(1, "Daft Punk", "One More Time", "Discovery")
		Expect(m.AlreadyHave(adminCtx, "Daft Punk", "")).To(BeEmpty())
	})
})
