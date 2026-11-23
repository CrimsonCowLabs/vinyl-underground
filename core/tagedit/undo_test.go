package tagedit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/audiodata"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

var _ = Describe("Undo", func() {
	var (
		ctx      context.Context
		ds       *tests.MockDataStore
		journal  *tests.MockEditJournalRepo
		editor   *Editor
		libDir   string
		path     string
		pristine string
	)

	// rescanned records the File's size and modification time on its Track, as a scan does.
	rescanned := func() {
		GinkgoHelper()
		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		ds.MockedMediaFile.(*tests.MockMediaFileRepo).SetData(model.MediaFiles{{
			ID: trackID, LibraryID: 1, Path: filepath.ToSlash(filepath.Join(relDir, filepath.Base(path))),
			Title: "Song", Suffix: strings.TrimPrefix(filepath.Ext(path), "."), Size: info.Size(), UpdatedAt: info.ModTime(),
		}})
	}
	tagsOf := func(p string) map[string][]string {
		GinkgoHelper()
		tags, err := taglib.ReadTags(p)
		Expect(err).ToNot(HaveOccurred())
		return tags
	}
	// edit edits the Track, and rescans it as the app does after a write, returning the entry.
	edit := func(vals Values) string {
		GinkgoHelper()
		res, err := editor.Edit(ctx, trackID, vals)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Changed).To(BeTrue())
		rescanned()
		return res.Entry
	}
	undo := func(entryID string) *Result {
		GinkgoHelper()
		res, err := editor.Undo(ctx, entryID)
		Expect(err).ToNot(HaveOccurred())
		rescanned()
		return res
	}

	BeforeEach(func() {
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", IsAdmin: true})
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

		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.MkdirAll(filepath.Join(libDir, relDir), 0o755)).To(Succeed())
		path = filepath.Join(libDir, relDir, "01 Song.mp3")
		Expect(os.WriteFile(path, data, 0o644)).To(Succeed())
		pristine = filepath.Join(GinkgoT().TempDir(), "01 Song.mp3")
		Expect(os.WriteFile(pristine, data, 0o644)).To(Succeed())
		rescanned()
	})

	It("writes the original values back into the File, and the Track keeps its ID", func() {
		original := tagsOf(path)
		entry := edit(Values{"title": {"Song (Live)"}, "tracknumber": {"7"}, "comment": nil, "bpm": {"128"}})
		Expect(tagsOf(path)).To(HaveKeyWithValue("TITLE", []string{"Song (Live)"}))

		res := undo(entry)

		Expect(res.Changed).To(BeTrue())
		Expect(res.Track.ID).To(Equal(trackID))
		Expect(tagsOf(path)).To(Equal(original))
		same, err := audiodata.Same(pristine, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(same).To(BeTrue())

		u, err := journal.Get(res.Entry)
		Expect(err).ToNot(HaveOccurred())
		Expect(u.UndoOf).To(Equal(entry))
		Expect(u.State).To(Equal(model.EditWritten))
		Expect(u.Kind).To(Equal(model.EditTags))
		Expect(u.MediaFileID).To(Equal(trackID))
		Expect(u.UserID).To(Equal("admin-1"))
		Expect(u.Changes).To(HaveKeyWithValue("TITLE", original["TITLE"]))
		Expect(u.OriginalTags).To(HaveKeyWithValue("TITLE", []string{"Song (Live)"}))
	})

	It("undoes an undo, which redoes the edit, and journals each", func() {
		entry := edit(Values{"title": {"Song (Live)"}, "bpm": {"128"}})
		edited := tagsOf(path)
		undone := undo(entry).Entry

		redo := undo(undone)

		Expect(tagsOf(path)).To(Equal(edited))
		r, err := journal.Get(redo.Entry)
		Expect(err).ToNot(HaveOccurred())
		Expect(r.UndoOf).To(Equal(undone))
		Expect(r.Changes).To(Equal(model.EditTagValues{"TITLE": {"Song (Live)"}, "BPM": {"128"}}))
		Expect(journal.All()).To(HaveLen(3))
	})

	Describe("refuses, and leaves the File as it is,", func() {
		var entry string
		var before []byte
		BeforeEach(func() {
			entry = edit(Values{"title": {"Song (Live)"}, "bpm": {"128"}})
			var err error
			before, err = os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
		})
		untouched := func() {
			GinkgoHelper()
			got, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(before))
		}

		It("when a later edit changed the same fields", func() {
			later := edit(Values{"title": {"Song (Remix)"}})
			before, _ = os.ReadFile(path)

			_, err := editor.Undo(ctx, entry)

			Expect(err).To(MatchError(ErrChangedLater))
			Expect(err.Error()).To(ContainSubstring("TITLE"))
			untouched()
			Expect(journal.All()).To(HaveLen(2))
			// The later edit can be undone, and then the first.
			undo(later)
			undo(entry)
			Expect(tagsOf(path)).To(HaveKeyWithValue("TITLE", []string{"Song"}))
		})

		It("unless the later edit changed other fields only", func() {
			edit(Values{"comment": {"a comment"}})

			undo(entry)

			Expect(tagsOf(path)).To(HaveKeyWithValue("TITLE", []string{"Song"}))
			Expect(tagsOf(path)).To(HaveKeyWithValue("COMMENT", []string{"a comment"}))
		})

		It("when the entry was undone already", func() {
			undo(entry)
			before, _ = os.ReadFile(path)
			Expect(editor.Undo(ctx, entry)).Error().To(MatchError(ErrAlreadyUndone))
			untouched()
		})

		It("when a later write to the File is unfinished", func() {
			Expect(journal.Put(&model.EditEntry{Kind: model.EditTags, State: model.EditPending, LibraryID: 1,
				MediaFileID: trackID, Path: "Artist/Album/01 Song.mp3", NewPath: "Artist/Album/01 Song.mp3",
				Changes: model.EditTagValues{"COMMENT": {"x"}}})).To(Succeed())
			Expect(editor.Undo(ctx, entry)).Error().To(MatchError(ErrUnfinished))
			untouched()
		})

		It("when the File changed on disk since the last scan", func() {
			later := time.Now().Add(time.Hour)
			Expect(os.Chtimes(path, later, later)).To(Succeed())
			before, _ = os.ReadFile(path)

			Expect(editor.Undo(ctx, entry)).Error().To(MatchError(ErrChangedOnDisk))
			untouched()
			Expect(journal.All()).To(HaveLen(1))
		})

		It("when the File's tags were changed outside the app, and rescanned", func() {
			Expect(taglib.WriteTags(path, map[string][]string{"TITLE": {"Other"}}, 0)).To(Succeed())
			rescanned()
			before, _ = os.ReadFile(path)

			Expect(editor.Undo(ctx, entry)).Error().To(MatchError(ErrTagsChanged))
			untouched()
		})

		It("an entry that was never written", func() {
			Expect(journal.Put(&model.EditEntry{Kind: model.EditTags, State: model.EditFailed, LibraryID: 1,
				MediaFileID: trackID, Path: "Artist/Album/01 Song.mp3", NewPath: "Artist/Album/01 Song.mp3",
				Changes: model.EditTagValues{"TITLE": {"x"}}})).To(Succeed())
			failed := journal.All()[1].ID
			Expect(editor.Undo(ctx, failed)).Error().To(MatchError(ErrNotUndoable))
			untouched()
		})

		It("while editing is off", func() {
			core.SetLibraryEditing(core.LibraryEditing{})
			Expect(editor.Undo(ctx, entry)).Error().To(MatchError(ErrEditingOff))
			untouched()
		})
	})
})
