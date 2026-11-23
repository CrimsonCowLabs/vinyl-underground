package tagedit

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/audiodata"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

// spoilingTagger is TagLib, but its picture write changes a tag too.
type spoilingTagger struct{ Tagger }

func (s spoilingTagger) WritePicture(path string, p *Picture) error {
	if err := s.Tagger.WritePicture(path, p); err != nil {
		return err
	}
	return s.Tagger.Write(path, map[string][]string{"TITLE": {"Spoiled"}})
}

var _ = Describe("Embed in Files", func() {
	var (
		ctx      context.Context
		ds       *tests.MockDataStore
		journal  *tests.MockEditJournalRepo
		editor   *Editor
		libDir   string
		path     string
		pristine string
		cover    []byte
	)

	rescanned := func() {
		GinkgoHelper()
		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		ds.MockedMediaFile.(*tests.MockMediaFileRepo).SetData(model.MediaFiles{{
			ID: trackID, LibraryID: 1, Path: filepath.ToSlash(filepath.Join(relDir, filepath.Base(path))),
			Title: "Song", Suffix: strings.TrimPrefix(filepath.Ext(path), "."), Size: info.Size(), UpdatedAt: info.ModTime(),
		}})
	}
	place := func(fixture string) {
		GinkgoHelper()
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", fixture))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.MkdirAll(filepath.Join(libDir, relDir), 0o755)).To(Succeed())
		path = filepath.Join(libDir, relDir, "01 Song"+filepath.Ext(fixture))
		Expect(os.WriteFile(path, data, 0o644)).To(Succeed())
		pristine = filepath.Join(GinkgoT().TempDir(), filepath.Base(path))
		Expect(os.WriteFile(pristine, data, 0o644)).To(Succeed())
		rescanned()
	}
	pictures := func(p string) []Picture {
		GinkgoHelper()
		pics, err := taglibTagger{}.Pictures(p)
		Expect(err).ToNot(HaveOccurred())
		return pics
	}
	data := func(pics []Picture) [][]byte {
		var res [][]byte
		for _, p := range pics {
			res = append(res, p.Data)
		}
		return res
	}
	tagsOf := func(p string) map[string][]string {
		GinkgoHelper()
		tags, err := taglib.ReadTags(p)
		Expect(err).ToNot(HaveOccurred())
		return tags
	}
	embed := func() *Result {
		GinkgoHelper()
		res, err := editor.EmbedCoverInJob(ctx, "job-1", trackID, cover)
		Expect(err).ToNot(HaveOccurred())
		rescanned()
		return res
	}
	sameAudio := func() {
		GinkgoHelper()
		same, err := audiodata.Same(pristine, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(same).To(BeTrue())
	}
	noHidden := func() {
		GinkgoHelper()
		entries, err := os.ReadDir(filepath.Dir(path))
		Expect(err).ToNot(HaveOccurred())
		for _, e := range entries {
			Expect(e.Name()).ToNot(HavePrefix("."))
		}
	}

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", IsAdmin: true})
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
		libDir = GinkgoT().TempDir()
		ds = &tests.MockDataStore{}
		ds.MockedLibrary = &tests.MockLibraryRepo{}
		ds.MockedLibrary.(*tests.MockLibraryRepo).SetData(model.Libraries{{ID: 1, Name: "NAS", Path: libDir}})
		ds.MockedMediaFile = tests.CreateMockMediaFileRepo()
		journal = &tests.MockEditJournalRepo{}
		ds.MockedEditJournal = journal
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS", Writable: true}}})
		DeferCleanup(func() { core.SetLibraryEditing(core.LibraryEditing{}) })
		editor = New(ds, tests.NewMockScanner())
		cover = fixtureImage("cover.jpg")
	})

	DescribeTable("puts the cover first among the File's pictures, verified, and the undo puts them back",
		func(fixture string) {
			place(fixture)
			before, tags := pictures(path), tagsOf(path)
			Expect(before).ToNot(BeEmpty(), "the fixture has a picture")

			res := embed()
			Expect(res.Changed).To(BeTrue())
			after := pictures(path)
			Expect(data(after)).To(Equal(append([][]byte{cover}, data(before[1:])...)), "the first is replaced, the others kept")
			Expect(tagsOf(path)).To(Equal(tags))
			sameAudio()
			noHidden()

			e, err := journal.Get(res.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(e.Kind).To(Equal(model.EditEmbed))
			Expect(e.State).To(Equal(model.EditWritten))
			Expect(e.JobID).To(Equal("job-1"))
			Expect(e.MediaFileID).To(Equal(trackID))
			Expect(e.Image.Sum).To(Equal(sumOfBytes(cover)))
			Expect(e.OriginalImage.Sum).To(Equal(sumOfBytes(before[0].Data)))

			undo, err := editor.Undo(ctx, res.Entry)
			Expect(err).ToNot(HaveOccurred())
			rescanned()
			Expect(data(pictures(path))).To(Equal(data(before)))
			Expect(tagsOf(path)).To(Equal(tags))
			sameAudio()
			u, err := journal.Get(undo.Entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Kind).To(Equal(model.EditEmbed))
			Expect(u.UndoOf).To(Equal(res.Entry))
		},
		Entry("mp3", "test.mp3"),
		Entry("m4a", "test.m4a"),
		Entry("aiff", "test.aiff"),
		Entry("wav", "test.wav"),
	)

	It("adds the cover to a File with no picture, and the undo removes it, adding no ID3v1 tag", func() {
		place("rips/ripped.mp3")
		Expect(pictures(path)).To(BeEmpty())
		hadV1, err := hasID3v1(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(hadV1).To(BeFalse())

		res := embed()
		Expect(data(pictures(path))).To(Equal([][]byte{cover}))
		has, err := hasID3v1(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(has).To(BeFalse())

		_, err = editor.Undo(ctx, res.Entry)
		Expect(err).ToNot(HaveOccurred())
		Expect(pictures(path)).To(BeEmpty())
		sameAudio()
	})

	It("writes nothing to a File whose first picture is the cover already", func() {
		place("test.mp3")
		embed()
		res, err := editor.EmbedCoverInJob(ctx, "job-2", trackID, cover)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Changed).To(BeFalse())
		Expect(journal.All()).To(HaveLen(1))
	})

	It("leaves the File untouched when the written copy changed a tag too", func() {
		place("test.mp3")
		editor.tagger = spoilingTagger{Tagger: taglibTagger{}}
		_, err := editor.EmbedCoverInJob(ctx, "job-1", trackID, cover)
		Expect(err).To(MatchError(ErrNotVerified))
		got, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		want, err := os.ReadFile(pristine)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(want))
		noHidden()
		Expect(journal.All()).To(ConsistOf(HaveField("State", model.EditFailed)))
	})

	It("refuses a File that changed on disk since the scan", func() {
		place("test.mp3")
		Expect(os.WriteFile(path, append(must(os.ReadFile(path)), 0), 0o644)).To(Succeed())
		_, err := editor.EmbedCoverInJob(ctx, "job-1", trackID, cover)
		Expect(err).To(MatchError(ErrChangedOnDisk))
	})

	It("refuses the undo when the File's picture was changed since", func() {
		place("test.mp3")
		res := embed()
		Expect(taglib.WriteImage(path, fixtureImage("front.png"))).To(Succeed())
		rescanned()
		_, err := editor.Undo(ctx, res.Entry)
		Expect(err).To(MatchError(ErrCoverChanged))
	})

	It("refuses the undo of an embed a later embed replaced, but not one a tag edit followed", func() {
		place("test.mp3")
		first := embed()
		_, err := editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})
		Expect(err).ToNot(HaveOccurred())
		rescanned()

		cover = fixtureImage("front.png")
		second := embed()
		_, err = editor.Undo(ctx, first.Entry)
		Expect(err).To(MatchError(ErrChangedLater))

		_, err = editor.Undo(ctx, second.Entry)
		Expect(err).ToNot(HaveOccurred())
		rescanned()
		_, err = editor.Undo(ctx, first.Entry)
		Expect(err).ToNot(HaveOccurred())
		Expect(tagsOf(path)).To(HaveKeyWithValue("TITLE", []string{"Song (Live)"}), "the tag edit stays")
	})
})

func must[T any](v T, err error) T {
	GinkgoHelper()
	Expect(err).ToNot(HaveOccurred())
	return v
}
