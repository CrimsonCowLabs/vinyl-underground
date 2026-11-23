package organize

import (
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/librarymanager"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Plan", func() {
	var (
		l *library
		o *Organizer
	)

	BeforeEach(func() {
		l = newLibrary()
		o = New(l.ds, nil, nil)
	})

	plan := func(scope Scope) *Plan {
		GinkgoHelper()
		p, err := o.Plan(l.ctx, scope)
		Expect(err).ToNot(HaveOccurred())
		return p
	}
	everything := Scope{Selection: librarymanager.Selection{Filter: map[string]any{}}}
	// moves are the plan's moves, old path: new path.
	moves := func(p *Plan) map[string]string {
		res := map[string]string{}
		for _, m := range p.Moves {
			res[m.Path] = m.NewPath
		}
		return res
	}
	// unplaced are the Files the plan leaves, path: reason.
	unplaced := func(p *Plan) map[string]string {
		res := map[string]string{}
		for _, m := range p.Unplaced {
			res[m.Path] = m.Status
		}
		return res
	}
	song := func(title string, track int) model.MediaFile {
		return model.MediaFile{Title: title, Artist: "Band", AlbumArtist: "Band", Album: "Record", Year: 1999, TrackNumber: track}
	}

	It("names each File <Album Artist>/<Album> (<Year>)/<NN> <Title>.<ext>, wherever it is now", func() {
		l.track("iTunes_Library/Band/Record/a.mp3", song("Song", 1))
		l.track("iTunes_Library/Band/Record/b.M4A", song("Other Song", 12))
		p := plan(everything)
		Expect(moves(p)).To(Equal(map[string]string{
			"iTunes_Library/Band/Record/a.mp3": "Band/Record (1999)/01 Song.mp3",
			"iTunes_Library/Band/Record/b.M4A": "Band/Record (1999)/12 Other Song.m4a",
		}))
		Expect(p.Unplaced).To(BeEmpty())
		Expect(p.Token).ToNot(BeEmpty())
	})

	It("numbers a multi-disc album's Files <D>-<NN>", func() {
		one := song("One", 1)
		one.DiscNumber = 1
		two := song("Two", 1)
		two.DiscNumber = 2
		l.track("Band/Record/CD1/01.mp3", one)
		l.track("Band/Record/CD2/01.mp3", two)
		Expect(moves(plan(everything))).To(Equal(map[string]string{
			"Band/Record/CD1/01.mp3": "Band/Record (1999)/1-01 One.mp3",
			"Band/Record/CD2/01.mp3": "Band/Record (1999)/2-01 Two.mp3",
		}))
	})

	It("uses the album's original year, as the album page shows it, and no year when it has none", func() {
		a := song("A", 1)
		a.OriginalYear = 1971
		l.track("x/a.mp3", a)
		b := song("B", 1)
		b.Album, b.Year = "Demos", 0
		l.track("x/b.mp3", b)
		Expect(moves(plan(everything))).To(Equal(map[string]string{
			"x/a.mp3": "Band/Record (1971)/01 A.mp3",
			"x/b.mp3": "Band/Demos/01 B.mp3",
		}))
	})

	It("makes each name safe for the NAS, as Promote does", func() {
		s := song(`What? "Live"`, 3)
		s.AlbumArtist, s.Album = "AC/DC", "Back. "
		l.track("x/a.mp3", s)
		Expect(moves(plan(everything))).To(HaveKeyWithValue("x/a.mp3", "AC_DC/Back (1999)/03 What_ _Live_.mp3"))
	})

	It("leaves a File already in place, also when its name differs only in letter case", func() {
		l.track("Band/Record (1999)/01 Song.mp3", song("Song", 1))
		l.track("Band/Record (1999)/02 other song.mp3", song("Other Song", 2))
		p := plan(everything)
		Expect(p.Moves).To(BeEmpty())
		Expect(p.InPlace).To(Equal(2))
	})

	Describe("letter case", func() {
		It("reuses an existing folder whose name differs only in letter case", func() {
			l.folders("BAND/record (1999)")
			l.track("x/a.mp3", song("Song", 1))
			Expect(moves(plan(everything))).To(HaveKeyWithValue("x/a.mp3", "BAND/record (1999)/01 Song.mp3"))
		})

		It("makes one folder for album artists that differ only in letter case", func() {
			a := song("A", 1)
			b := song("B", 2)
			b.AlbumArtist = "BAND"
			l.track("x/a.mp3", a)
			l.track("y/b.mp3", b)
			Expect(moves(plan(everything))).To(Equal(map[string]string{
				"x/a.mp3": "Band/Record (1999)/01 A.mp3",
				"y/b.mp3": "Band/Record (1999)/02 B.mp3",
			}), "the first Track in path order names it")
		})

		It("leaves Files whose old or new folder has a twin differing only in letter case", func() {
			l.folders("Band")
			l.folders("BAND")
			l.track("x/a.mp3", song("Song", 1))
			l.folders("Dire Straits")
			l.folders("DIRE STRAITS")
			s := song("Money", 2)
			s.AlbumArtist, s.Album = "Other", "Thing"
			l.track("DIRE STRAITS/x/b.mp3", s)
			Expect(unplaced(plan(everything))).To(Equal(map[string]string{
				"x/a.mp3":              ReasonCaseTwin,
				"DIRE STRAITS/x/b.mp3": ReasonCaseTwin,
			}))
		})
	})

	Describe("collisions", func() {
		It("flags two Files planned for one path, and moves neither", func() {
			l.track("x/a.mp3", song("Song", 1))
			l.track("x/a 1.mp3", song("Song", 1))
			p := plan(everything)
			Expect(p.Moves).To(BeEmpty())
			Expect(unplaced(p)).To(Equal(map[string]string{"x/a.mp3": ReasonSamePath, "x/a 1.mp3": ReasonSamePath}))
		})

		It("flags a File whose new path is taken, by a Track or any File, in any letter case", func() {
			l.track("x/a.mp3", song("Song", 1))
			l.file("Band/Record (1999)/01 SONG.mp3", "not scanned")
			Expect(unplaced(plan(everything))).To(Equal(map[string]string{"x/a.mp3": ReasonPathTaken}))
		})
	})

	DescribeTable("lists the Tracks it can't place, with why",
		func(change func(*model.MediaFile), reason string) {
			mf := song("Song", 1)
			change(&mf)
			l.track("x/a.mp3", mf)
			p := plan(everything)
			Expect(p.Moves).To(BeEmpty())
			Expect(unplaced(p)).To(Equal(map[string]string{"x/a.mp3": reason}))
		},
		Entry("no album artist", func(mf *model.MediaFile) { mf.AlbumArtist = "" }, ReasonNoAlbumArtist),
		Entry("an unknown album artist", func(mf *model.MediaFile) { mf.AlbumArtist = "[Unknown Artist]" }, ReasonNoAlbumArtist),
		Entry("no album", func(mf *model.MediaFile) { mf.Album = "" }, ReasonNoAlbum),
		Entry("an unknown album", func(mf *model.MediaFile) { mf.Album = "Unknown Album" }, ReasonNoAlbum),
		Entry("no title", func(mf *model.MediaFile) { mf.Title = "" }, ReasonNoTitle),
		Entry("a title that is only a number", func(mf *model.MediaFile) { mf.Title = "05" }, ReasonNoTitle),
		Entry(`a title "Track NN"`, func(mf *model.MediaFile) { mf.Title = "Track 01" }, ReasonNoTitle),
		Entry("a title still starting with its track number", func(mf *model.MediaFile) { mf.Title = "01 Song" }, ReasonTitleNumber),
		Entry("a title starting with a number retag couldn't tell (FlagNumberKept)", func(mf *model.MediaFile) {
			mf.Title, mf.TrackNumber = "1 Song", 0
		}, ReasonTitleNumber),
		Entry("no track number", func(mf *model.MediaFile) { mf.TrackNumber = 0 }, ReasonNoTrackNumber),
	)

	It("places a Track whose title only starts with a number that isn't its track", func() {
		l.track("x/a.mp3", song("7 Seconds", 3))
		Expect(moves(plan(everything))).To(HaveKeyWithValue("x/a.mp3", "Band/Record (1999)/03 7 Seconds.mp3"))
	})

	It("leaves a multi-disc album's Track with no disc number", func() {
		one := song("One", 1)
		two := song("Two", 1)
		two.DiscNumber = 2
		l.track("x/1.mp3", one)
		l.track("x/2.mp3", two)
		Expect(unplaced(plan(everything))).To(Equal(map[string]string{"x/1.mp3": ReasonNoDiscNumber}))
	})

	It("leaves a Track with no album artist of its own whose album's other Tracks in its folder have other artists", func() {
		a := song("A", 1)
		a.AlbumArtist, a.Artist, a.AlbumID = "Singer", "Singer", "al-a"
		b := song("B", 2)
		b.AlbumArtist, b.Artist, b.AlbumID = "Rapper", "Rapper", "al-b"
		mfA := l.track("Mix/a.mp3", a)
		mfB := l.track("Mix/b.mp3", b)
		for _, mf := range []*model.MediaFile{mfA, mfB} {
			mf.HasAlbumArtist = false
			Expect(l.ds.MediaFile(l.ctx).Put(mf)).To(Succeed())
		}
		Expect(unplaced(plan(everything))).To(Equal(map[string]string{"Mix/a.mp3": ReasonNoAlbumArtist, "Mix/b.mp3": ReasonNoAlbumArtist}))
	})

	Describe("the album's other Files", func() {
		It("moves the album's folder with it: the images the scan found are listed, the folder's other Files go too", func() {
			l.track("iTunes/Band/Record/a.mp3", song("Song", 1))
			l.file("iTunes/Band/Record/cover.jpg", "image")
			l.file("iTunes/Band/Record/.DS_Store", "junk")
			l.file("iTunes/Band/Record/Scans/back.png", "scan")
			p := plan(everything)
			Expect(moves(p)).To(Equal(map[string]string{
				"iTunes/Band/Record/a.mp3":          "Band/Record (1999)/01 Song.mp3",
				"iTunes/Band/Record/cover.jpg":      "Band/Record (1999)/cover.jpg",
				"iTunes/Band/Record/Scans/back.png": "Band/Record (1999)/Scans/back.png",
			}))
			Expect(p.Folders).To(Equal([]FolderMove{{LibraryID: 1, Path: "iTunes/Band/Record", NewPath: "Band/Record (1999)"}}))
		})

		It("moves the Files of the folder above a multi-disc album's disc folders, keeping their place", func() {
			one := song("One", 1)
			one.DiscNumber = 1
			two := song("Two", 1)
			two.DiscNumber = 2
			l.track("x/Record/CD1/01.mp3", one)
			l.track("x/Record/CD2/01.mp3", two)
			l.file("x/Record/cover.jpg", "image")
			l.file("x/Record/CD1/cover.jpg", "disc image")
			Expect(moves(plan(everything))).To(Equal(map[string]string{
				"x/Record/CD1/01.mp3":    "Band/Record (1999)/1-01 One.mp3",
				"x/Record/CD2/01.mp3":    "Band/Record (1999)/2-01 Two.mp3",
				"x/Record/cover.jpg":     "Band/Record (1999)/cover.jpg",
				"x/Record/CD1/cover.jpg": "Band/Record (1999)/CD1/cover.jpg",
			}))
		})

		It("leaves them in a folder holding another album's Tracks", func() {
			l.track("Mix/a.mp3", song("Song", 1))
			other := song("Else", 1)
			other.Album = "Other"
			l.track("Mix/b.mp3", other)
			l.file("Mix/cover.jpg", "image")
			p := plan(everything)
			Expect(moves(p)).ToNot(HaveKey("Mix/cover.jpg"))
			Expect(p.Folders).To(BeEmpty())
		})

		It("leaves them when only some of the album's Tracks are selected", func() {
			a := l.track("x/a.mp3", song("A", 1))
			l.track("x/b.mp3", song("B", 2))
			l.file("x/cover.jpg", "image")
			Expect(moves(plan(Scope{Selection: librarymanager.Selection{IDs: []string{a.ID}}}))).To(Equal(map[string]string{
				"x/a.mp3": "Band/Record (1999)/01 A.mp3",
			}))
		})

		It("leaves them when an album's Track can't be placed", func() {
			l.track("x/a.mp3", song("A", 1))
			l.track("x/b.mp3", song("", 2))
			l.file("x/cover.jpg", "image")
			Expect(moves(plan(everything))).ToNot(HaveKey("x/cover.jpg"))
		})

		It("flags one whose new path is taken", func() {
			l.track("x/a.mp3", song("A", 1))
			l.file("x/cover.jpg", "image")
			l.file("Band/Record (1999)/cover.jpg", "another image")
			p := plan(everything)
			Expect(unplaced(p)).To(Equal(map[string]string{"x/cover.jpg": ReasonPathTaken}))
			Expect(p.Folders).To(Equal([]FolderMove{{LibraryID: 1, Path: "x", NewPath: "Band/Record (1999)", Keep: []string{"x/cover.jpg"}}}))
		})
	})

	It("plans an album, from the album page", func() {
		l.track("x/a.mp3", song("A", 1))
		other := song("B", 1)
		other.Album = "Other"
		l.track("y/b.mp3", other)
		Expect(moves(plan(Scope{AlbumID: "Band|Record"}))).To(Equal(map[string]string{"x/a.mp3": "Band/Record (1999)/01 A.mp3"}))
	})

	It("leaves a Track whose library is read-only", func() {
		l.track("x/a.mp3", song("A", 1))
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: false}}})
		Expect(unplaced(plan(everything))).To(Equal(map[string]string{"x/a.mp3": ReasonEditingOff}))
	})

	It("names the same plan with the same token, and a changed one with another", func() {
		mf := l.track("x/a.mp3", song("A", 1))
		first := plan(everything).Token
		Expect(plan(everything).Token).To(Equal(first))
		mf.Title = "B"
		Expect(l.ds.MediaFile(l.ctx).Put(mf)).To(Succeed())
		Expect(plan(everything).Token).ToNot(Equal(first))
	})
})
