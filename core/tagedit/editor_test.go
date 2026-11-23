package tagedit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/audiodata"
	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

const (
	trackID = "track-1"
	relDir  = "Artist/Album"
)

var errInjected = errors.New("injected failure")

// fakeTagger is the real TagLib, with hooks to see or spoil what a write does.
type fakeTagger struct {
	Tagger
	beforeWrite func(path string)
	afterWrite  func(path string)
}

func (f fakeTagger) Write(path string, set map[string][]string) error {
	if f.beforeWrite != nil {
		f.beforeWrite(path)
	}
	if err := f.Tagger.Write(path, set); err != nil {
		return err
	}
	if f.afterWrite != nil {
		f.afterWrite(path)
	}
	return nil
}

// faultyFS fails a rename of a File whose path contains match.
type faultyFS struct {
	safefile.FS
	match string
}

func (f faultyFS) Rename(oldname, newname string) error {
	if strings.Contains(oldname, f.match) {
		return errInjected
	}
	return f.FS.Rename(oldname, newname)
}

// undeletableFS fails a removal of a File whose path contains match.
type undeletableFS struct {
	safefile.FS
	match string
}

func (f undeletableFS) Remove(name string) error {
	if strings.Contains(name, f.match) {
		return errInjected
	}
	return f.FS.Remove(name)
}

var _ = Describe("Editor", func() {
	var (
		ctx      context.Context
		ds       *tests.MockDataStore
		journal  *tests.MockEditJournalRepo
		scanner  *tests.MockScanner
		editor   *Editor
		libDir   string
		path     string // the File, absolute
		pristine string // an untouched copy of it, outside the library
		// rescanned records the File's size and modification time on its Track, as a scan does.
		rescanned func()
	)

	// track puts the fixture in the library as a scanned Track, as the scanner would record it.
	place := func(fixture string, withSource bool) {
		GinkgoHelper()
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", fixture))
		Expect(err).ToNot(HaveOccurred())
		name := "01 Song" + filepath.Ext(fixture)
		Expect(os.MkdirAll(filepath.Join(libDir, relDir), 0o755)).To(Succeed())
		path = filepath.Join(libDir, relDir, name)
		Expect(os.WriteFile(path, data, 0o644)).To(Succeed())
		if withSource {
			// Tags the app doesn't map, which must survive: a Rip's source and its page.
			Expect(taglib.WriteTags(path, map[string][]string{"SOURCE": {"YouTube"}, "SOURCEURL": {"https://www.youtube.com/watch?v=x"}}, 0)).To(Succeed())
		}
		pristine = filepath.Join(GinkgoT().TempDir(), name)
		data, err = os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(pristine, data, 0o644)).To(Succeed())
		rescanned()
	}
	track := func(fixture string) {
		GinkgoHelper()
		place(fixture, true)
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
		scanner = tests.NewMockScanner()
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS", Writable: true}}})
		DeferCleanup(func() { core.SetLibraryEditing(core.LibraryEditing{}) })
		editor = New(ds, scanner)
	})

	rescanned = func() {
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
	// hiddenFiles are the temp and backup Files an edit must never leave behind.
	hiddenFiles := func() []string {
		GinkgoHelper()
		entries, err := os.ReadDir(filepath.Dir(path))
		Expect(err).ToNot(HaveOccurred())
		var res []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				res = append(res, e.Name())
			}
		}
		return res
	}
	unchanged := func() {
		GinkgoHelper()
		got, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		want, err := os.ReadFile(pristine)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(want), "the File was changed")
		Expect(hiddenFiles()).To(BeEmpty())
	}

	DescribeTable("writes only the changed fields, keeps every other tag and the audio",
		func(fixture string) {
			track(fixture)
			before := tagsOf(path)

			res, err := editor.Edit(ctx, trackID, Values{
				"title":       {"Song (Live)"},
				"tracknumber": {"7"},
				"artists":     {"Artist One", "Artist Two"},
				"lyrics":      {"la la la"},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Changed).To(BeTrue())

			after := tagsOf(path)
			Expect(after).To(HaveKeyWithValue("TITLE", []string{"Song (Live)"}))
			Expect(after).To(HaveKeyWithValue("TRACKNUMBER", []string{"7/10"}), "the track total is kept")
			Expect(after).To(HaveKeyWithValue("ARTISTS", []string{"Artist One", "Artist Two"}))
			Expect(after).To(HaveKeyWithValue("LYRICS", []string{"la la la"}))
			for k, v := range before {
				if k == "TITLE" || k == "TRACKNUMBER" || k == "ARTISTS" || k == "LYRICS" {
					continue
				}
				Expect(after).To(HaveKeyWithValue(k, v), "tag %s", k)
			}
			Expect(after).To(HaveKeyWithValue("SOURCE", []string{"YouTube"}))
			Expect(after).To(HaveKeyWithValue("SOURCEURL", []string{"https://www.youtube.com/watch?v=x"}))
			Expect(after).To(HaveKey("REPLAYGAIN_TRACK_GAIN"))

			same, err := audiodata.Same(pristine, path)
			Expect(err).ToNot(HaveOccurred())
			Expect(same).To(BeTrue())
			Expect(hiddenFiles()).To(BeEmpty())
		},
		Entry("mp3", "test.mp3"),
		Entry("m4a", "test.m4a"),
		Entry("aiff", "test.aiff"),
		Entry("wav", "test.wav"),
	)

	DescribeTable("can set and remove every field it shows",
		func(fixture string) {
			track(fixture)
			set := Values{
				"title": {"T"}, "artist": {"A"}, "artists": {"A1", "A2"}, "album": {"Al"},
				"albumartist": {"AA"}, "albumartists": {"AA1", "AA2"}, "date": {"2001-02-03"},
				"originaldate": {"1999"}, "tracknumber": {"3"}, "tracktotal": {"12"}, "discnumber": {"2"},
				"disctotal": {"3"}, "genre": {"Rock", "Pop"}, "compilation": {"1"}, "comment": {"c"},
				"bpm": {"120"}, "lyrics": {"line one\nline two"},
				"musicbrainz_trackid":        {"8f8b1b1b-1b1b-4b1b-8b1b-1b1b1b1b1b1b"},
				"musicbrainz_releasetrackid": {"8f8b1b1b-1b1b-4b1b-8b1b-1b1b1b1b1b1c"},
				"musicbrainz_albumid":        {"8f8b1b1b-1b1b-4b1b-8b1b-1b1b1b1b1b1d"},
				"musicbrainz_artistid":       {"8f8b1b1b-1b1b-4b1b-8b1b-1b1b1b1b1b1e"},
				"musicbrainz_albumartistid":  {"8f8b1b1b-1b1b-4b1b-8b1b-1b1b1b1b1b1f"},
				"musicbrainz_releasegroupid": {"8f8b1b1b-1b1b-4b1b-8b1b-1b1b1b1b1b10"},
			}
			Expect(editor.Edit(ctx, trackID, set)).Error().ToNot(HaveOccurred())
			got, err := editor.Read(ctx, trackID)
			Expect(err).ToNot(HaveOccurred())
			for k, v := range set {
				Expect(got.Values).To(HaveKeyWithValue(k, v), "field %s", k)
			}

			rescanned()
			cleared := Values{}
			for k := range set {
				cleared[k] = nil
			}
			Expect(editor.Edit(ctx, trackID, cleared)).Error().ToNot(HaveOccurred())
			got, err = editor.Read(ctx, trackID)
			Expect(err).ToNot(HaveOccurred())
			for k := range set {
				Expect(got.Values[k]).To(BeEmpty(), "field %s", k)
			}
		},
		Entry("mp3", "test.mp3"),
		Entry("m4a", "test.m4a"),
		Entry("aiff", "test.aiff"),
		Entry("wav", "test.wav"),
	)

	Describe("mp3", func() {
		id3Version := func() byte {
			GinkgoHelper()
			b, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(b[:3])).To(Equal("ID3"))
			return b[3]
		}
		hasID3v1 := func() bool {
			GinkgoHelper()
			b, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			return string(b[len(b)-128:len(b)-125]) == "TAG"
		}

		It("is written as ID3v2.4", func() {
			place("01 Invisible (RED) Edit Version.mp3", false) // ID3v2.2
			Expect(id3Version()).To(Equal(byte(2)))
			Expect(editor.Edit(ctx, trackID, Values{"title": {"Invisible"}})).Error().ToNot(HaveOccurred())
			Expect(id3Version()).To(Equal(byte(4)))
		})

		It("updates an ID3v1 tag that is there", func() {
			track("test.mp3")
			Expect(hasID3v1()).To(BeTrue())
			Expect(editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})).Error().ToNot(HaveOccurred())
			v1, err := taglib.ReadID3v1Frames(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(v1).To(HaveKeyWithValue("TITLE", []string{"Song (Live)"}))
		})

		It("adds no ID3v1 tag to a File without one", func() {
			track("no_replaygain.mp3")
			Expect(hasID3v1()).To(BeFalse())
			Expect(editor.Edit(ctx, trackID, Values{"title": {"New"}})).Error().ToNot(HaveOccurred())
			Expect(hasID3v1()).To(BeFalse())
			Expect(tagsOf(path)).To(HaveKeyWithValue("TITLE", []string{"New"}))
		})
	})

	It("keeps an m4a's iTunes atoms", func() {
		track("test.m4a")
		before, err := taglib.ReadMP4Atoms(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})).Error().ToNot(HaveOccurred())
		after, err := taglib.ReadMP4Atoms(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(after).To(HaveKeyWithValue("©nam", []string{"Song (Live)"}))
		for k, v := range before {
			if k != "©nam" {
				Expect(after).To(HaveKeyWithValue(k, v), "atom %s", k)
			}
		}
	})

	Describe("the guard", func() {
		BeforeEach(func() { track("test.mp3") })

		It("refuses a File whose modification time differs from the scan's", func() {
			later := time.Now().Add(time.Hour)
			Expect(os.Chtimes(path, later, later)).To(Succeed())
			_, err := editor.Edit(ctx, trackID, Values{"title": {"x"}})
			Expect(err).To(MatchError(ErrChangedOnDisk))
			Expect(err.Error()).To(ContainSubstring("changed on disk, rescan first"))
			Expect(journal.All()).To(BeEmpty())
			Expect(tagsOf(path)).To(HaveKeyWithValue("TITLE", []string{"Song"}))
		})

		It("refuses a File whose size differs from the scan's", func() {
			mf, _ := ds.MediaFile(ctx).Get(trackID)
			mf.Size++
			ds.MockedMediaFile.(*tests.MockMediaFileRepo).SetData(model.MediaFiles{*mf})
			_, err := editor.Edit(ctx, trackID, Values{"title": {"x"}})
			Expect(err).To(MatchError(ErrChangedOnDisk))
			unchanged()
		})
	})

	Describe("the Edit journal", func() {
		BeforeEach(func() { track("test.mp3") })

		It("records the File's original tags before it is written", func() {
			original := tagsOf(path)
			var atWrite []model.EditEntry
			editor.tagger = fakeTagger{Tagger: editor.tagger, beforeWrite: func(string) { atWrite = journal.All() }}

			_, err := editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}, "comment": nil})
			Expect(err).ToNot(HaveOccurred())

			Expect(atWrite).To(HaveLen(1))
			Expect(atWrite[0].State).To(Equal(model.EditPending))
			Expect(atWrite[0].OriginalTags).To(Equal(model.EditTagValues(original)))
			Expect(atWrite[0].Changes).To(Equal(model.EditTagValues{"TITLE": {"Song (Live)"}, "COMMENT": nil}))

			e := journal.All()[0]
			Expect(e.State).To(Equal(model.EditWritten))
			Expect(e.Kind).To(Equal(model.EditTags))
			Expect(e.UserID).To(Equal("admin-1"))
			Expect(e.MediaFileID).To(Equal(trackID))
			Expect(e.LibraryID).To(Equal(1))
			Expect(e.Path).To(Equal("Artist/Album/01 Song.mp3"))
			Expect(e.NewPath).To(Equal(e.Path))
			Expect(e.JobID).ToNot(BeEmpty())
		})

		It("writes nothing when the journal can't record the edit", func() {
			journal.Err = errInjected
			_, err := editor.Edit(ctx, trackID, Values{"title": {"x"}})
			Expect(err).To(MatchError(errInjected))
			unchanged()
		})

		It("records nothing, and writes nothing, when nothing changed", func() {
			res, err := editor.Edit(ctx, trackID, Values{"title": {"Song"}})
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Changed).To(BeFalse())
			Expect(journal.All()).To(BeEmpty())
			unchanged()
		})
	})

	Describe("a failed write", func() {
		BeforeEach(func() { track("test.mp3") })

		failed := func() {
			GinkgoHelper()
			unchanged()
			Expect(journal.All()).To(HaveLen(1))
			Expect(journal.All()[0].State).To(Equal(model.EditFailed))
			Expect(journal.All()[0].Error).ToNot(BeEmpty())
		}

		It("leaves the File untouched when a field doesn't read back as written", func() {
			editor.tagger = fakeTagger{Tagger: editor.tagger, afterWrite: func(p string) {
				Expect(taglib.WriteTags(p, map[string][]string{"TITLE": {"something else"}}, 0)).To(Succeed())
			}}
			_, err := editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})
			Expect(err).To(MatchError(ErrNotVerified))
			failed()
		})

		It("leaves the File untouched when another tag changed too", func() {
			editor.tagger = fakeTagger{Tagger: editor.tagger, afterWrite: func(p string) {
				Expect(taglib.WriteTags(p, map[string][]string{"REPLAYGAIN_TRACK_GAIN": {"+9 dB"}}, 0)).To(Succeed())
			}}
			_, err := editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})
			Expect(err).To(MatchError(ErrNotVerified))
			failed()
		})

		It("leaves the File untouched when its audio data changed", func() {
			editor.tagger = fakeTagger{Tagger: editor.tagger, afterWrite: func(p string) {
				b, err := os.ReadFile(p)
				Expect(err).ToNot(HaveOccurred())
				b[len(b)-1000] ^= 0xff // in the last frames, before the ID3v1 tag
				Expect(os.WriteFile(p, b, 0o644)).To(Succeed())
			}}
			_, err := editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})
			Expect(err).To(MatchError(ErrNotVerified))
			failed()
		})

		It("leaves the File untouched when the copy can't take its place", func() {
			editor.fsys = faultyFS{FS: safefile.OS{}, match: "vu-edit"}
			_, err := editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})
			Expect(err).To(MatchError(errInjected))
			failed()
		})

		It("leaves the File untouched when the tags can't be written", func() {
			editor.tagger = fakeTagger{Tagger: editor.tagger, beforeWrite: func(p string) {
				Expect(os.WriteFile(p, []byte("not audio"), 0o644)).To(Succeed())
			}}
			_, err := editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})
			Expect(err).To(HaveOccurred())
			failed()
		})

		It("leaves the File untouched when a tag it sets isn't written at all", func() {
			editor.tagger = fakeTagger{Tagger: editor.tagger, afterWrite: func(p string) {
				Expect(taglib.WriteTags(p, map[string][]string{"ORIGINALDATE": nil}, 0)).To(Succeed())
			}}
			_, err := editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}, "originaldate": {"1990"}})
			Expect(err).To(MatchError(ErrNotVerified))
			failed()
		})
	})

	It("removes an earlier edit's old copy it couldn't remove then, and edits the File", func() {
		track("test.mp3")
		editor.fsys = undeletableFS{FS: safefile.OS{}, match: "vu-original"}
		Expect(editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})).Error().ToNot(HaveOccurred())
		Expect(hiddenFiles()).To(ConsistOf(filepath.Base(safefile.BackupName(path))))

		rescanned()
		editor.fsys = safefile.OS{}
		Expect(editor.Edit(ctx, trackID, Values{"title": {"Song (Live, 1990)"}})).Error().ToNot(HaveOccurred())
		Expect(tagsOf(path)).To(HaveKeyWithValue("TITLE", []string{"Song (Live, 1990)"}))
		Expect(hiddenFiles()).To(BeEmpty())
	})

	It("refuses to edit a File with an old copy beside it that no edit wrote", func() {
		track("test.mp3")
		Expect(os.WriteFile(safefile.BackupName(path), []byte("maybe the only good copy"), 0o644)).To(Succeed())
		_, err := editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})
		Expect(err).To(MatchError(ErrOldCopyLeft))
		Expect(tagsOf(path)).To(HaveKeyWithValue("TITLE", []string{"Song"}))
		Expect(hiddenFiles()).To(ConsistOf(filepath.Base(safefile.BackupName(path))))
	})

	It("keeps the File's permissions", func() {
		track("test.mp3")
		Expect(os.Chmod(path, 0o664)).To(Succeed())
		rescanned()
		Expect(editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})).Error().ToNot(HaveOccurred())
		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o664)))
	})

	It("refuses a File with other names (a hard link or a symlink), which a copy would split from it", func() {
		track("test.mp3")
		Expect(os.Link(path, filepath.Join(GinkgoT().TempDir(), "seeding.mp3"))).To(Succeed())
		_, err := editor.Edit(ctx, trackID, Values{"title": {"x"}})
		Expect(err).To(MatchError(safefile.ErrLinked))
		Expect(tagsOf(path)).To(HaveKeyWithValue("TITLE", []string{"Song"}))
	})

	It("rescans the File's folder, which re-reads it into the same Track", func() {
		track("test.mp3")
		Expect(editor.Edit(ctx, trackID, Values{"title": {"Song (Live)"}})).Error().ToNot(HaveOccurred())
		Expect(scanner.GetScanFoldersCalls()).To(ConsistOf(tests.ScanFoldersCall{
			FullScan: false, Targets: []model.ScanTarget{{LibraryID: 1, FolderPath: relDir}},
		}))
	})

	It("refuses invalid values", func() {
		track("test.mp3")
		for _, v := range []Values{
			{"tracknumber": {"seven"}}, {"bpm": {"fast"}}, {"compilation": {"maybe"}},
			{"date": {"last year"}}, {"musicbrainz_albumid": {"not-a-uuid"}}, {"title": {"a\x00b"}},
			{"nosuchfield": {"x"}}, {"title": {"one", "two"}},
		} {
			_, err := editor.Edit(ctx, trackID, v)
			Expect(err).To(MatchError(ErrInvalidTags), "%v", v)
		}
		unchanged()
	})

	It("is off when editing is off, or the Track's library is read-only", func() {
		track("test.mp3")
		core.SetLibraryEditing(core.LibraryEditing{})
		_, err := editor.Edit(ctx, trackID, Values{"title": {"x"}})
		Expect(err).To(MatchError(ErrEditingOff))
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Writable: false}, {ID: 2, Writable: true}}})
		_, err = editor.Edit(ctx, trackID, Values{"title": {"x"}})
		Expect(err).To(MatchError(ErrEditingOff))
		_, err = editor.Read(ctx, trackID)
		Expect(err).To(MatchError(ErrEditingOff))
		unchanged()
	})

	It("refuses a format whose audio it can't check", func() {
		track("test.flac")
		_, err := editor.Edit(ctx, trackID, Values{"title": {"x"}})
		Expect(err).To(MatchError(ErrUnsupportedFormat))
		unchanged()
	})

	Describe("Read", func() {
		It("shows the File's own tags, track and disc totals apart", func() {
			track("test.mp3")
			got, err := editor.Read(ctx, trackID)
			Expect(err).ToNot(HaveOccurred())
			Expect(got.ID).To(Equal(trackID))
			Expect(got.Values).To(HaveKeyWithValue("title", []string{"Song"}))
			Expect(got.Values).To(HaveKeyWithValue("tracknumber", []string{"2"}))
			Expect(got.Values).To(HaveKeyWithValue("tracktotal", []string{"10"}))
			Expect(got.Values).To(HaveKeyWithValue("discnumber", []string{"1"}))
			Expect(got.Values).To(HaveKeyWithValue("disctotal", []string{"2"}))
			Expect(got.Values).To(HaveKeyWithValue("genre", []string{"Rock"}))
			Expect(got.Fields).ToNot(BeEmpty())
		})
	})

	Describe("Settle", func() {
		It("puts back a File an edit left moved aside, and closes its journal entry", func() {
			track("test.mp3")
			Expect(os.Rename(path, safefile.BackupName(path))).To(Succeed())
			e := &model.EditEntry{Kind: model.EditTags, State: model.EditPending, LibraryID: 1,
				MediaFileID: trackID, Path: "Artist/Album/01 Song.mp3", CopySum: strings.Repeat("00", 32)}
			Expect(journal.Put(e)).To(Succeed())

			editor.Settle(ctx)

			unchanged()
			Expect(journal.All()[0].State).To(Equal(model.EditFailed))
		})

		It("removes the temp copy of an edit that never got to replace the File", func() {
			track("test.mp3")
			data, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(os.WriteFile(safefile.TempName(path), data, 0o644)).To(Succeed())
			Expect(journal.Put(&model.EditEntry{Kind: model.EditTags, State: model.EditPending, LibraryID: 1, Path: "Artist/Album/01 Song.mp3"})).To(Succeed())

			editor.Settle(ctx)

			unchanged()
			Expect(journal.All()[0].State).To(Equal(model.EditFailed))
		})
	})
})
