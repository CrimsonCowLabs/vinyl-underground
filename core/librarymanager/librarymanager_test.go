package librarymanager_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/librarymanager"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

// The Library manager's bulk edit (#166), on the real database (so "all matching" goes through
// the listing's own filters) and real Files, through the real edit job queue.
var _ = Describe("Library manager", Ordered, func() {
	var (
		ctx    context.Context
		ds     model.DataStore
		editor *tagedit.Editor
		q      *editjob.Queue
		m      *librarymanager.Manager
		libDir string
	)

	// The Tracks, as scanned. track-3 is a flac (which can't be edited yet), track-5's File is
	// missing.
	tracks := []model.MediaFile{
		{ID: "track-1", Path: "Artist/Album/01 One.mp3", Title: "One", Album: "Album", Artist: "Artist",
			AlbumArtist: "Artist", HasAlbumArtist: false, Tags: model.Tags{model.TagGenre: {"Rock"}}, Suffix: "mp3", Year: 1999, Date: "1999"},
		{ID: "track-2", Path: "Artist/Album/02 Two.mp3", Title: "Two", Album: "Album", Artist: "Artist",
			AlbumArtist: "The Artist", HasAlbumArtist: true, Suffix: "mp3", Year: 1999, Date: "1999"},
		{ID: "track-3", Path: "Artist/Album/03 Three.flac", Title: "Three", Album: "Album", Artist: "Artist",
			AlbumArtist: "Artist", Suffix: "flac", Year: 1999, Date: "1999"},
		{ID: "track-4", Path: "Other/04 Four.mp3", Title: "Four", Album: "Other", Artist: "Someone",
			AlbumArtist: "Someone", HasAlbumArtist: true, Suffix: "mp3"},
		{ID: "track-5", Path: "Artist/Album/05 Gone.mp3", Title: "Gone", Album: "Album", Artist: "Artist",
			AlbumArtist: "Artist", Suffix: "mp3", Missing: true},
	}

	BeforeAll(func() {
		conf.Server.DbPath = filepath.Join(GinkgoT().TempDir(), "library-manager.db?_journal_mode=WAL")
		db.Db().SetMaxOpenConns(1)
	})

	// scanned records each File's size and modification time on its Track, as a scan does.
	scanned := func() {
		GinkgoHelper()
		for i := range tracks {
			t := tracks[i]
			t.LibraryID = 1
			if info, err := os.Stat(filepath.Join(libDir, t.Path)); err == nil {
				t.Size, t.UpdatedAt = info.Size(), info.ModTime()
			}
			Expect(ds.MediaFile(ctx).Put(&t)).To(Succeed())
		}
	}

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		ctx = request.WithUser(context.Background(), model.User{ID: "admin-1", UserName: "admin", IsAdmin: true})
		libDir = GinkgoT().TempDir()
		conf.Server.MusicFolder = libDir
		db.Init(ctx)
		DeferCleanup(func() { Expect(tests.ClearDB()).To(Succeed()) })
		ds = persistence.New(db.Db())
		Expect(ds.User(ctx).Put(&model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pw"})).To(Succeed())

		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS", Writable: true}}})
		DeferCleanup(func() { core.SetLibraryEditing(core.LibraryEditing{}) })

		for _, t := range tracks {
			if t.Missing {
				continue
			}
			data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test."+t.Suffix))
			Expect(err).ToNot(HaveOccurred())
			p := filepath.Join(libDir, t.Path)
			Expect(os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
			Expect(os.WriteFile(p, data, 0o644)).To(Succeed())
		}
		scanned()

		editor = tagedit.New(ds, nil)
		q = editjob.New(ds, nil, editjob.Kinds(ds, editor))
		m = librarymanager.New(ds, q)
	})

	idsOf := func(mfs model.MediaFiles) []string {
		var out []string
		for _, mf := range mfs {
			out = append(out, mf.ID)
		}
		return out
	}

	Describe("selecting Tracks", func() {
		It("resolves all matching a filter on the server, in path order, without missing Files", func() {
			mfs, err := m.Tracks(ctx, librarymanager.Selection{Filter: map[string]any{"folder": "Artist/Album"}})
			Expect(err).ToNot(HaveOccurred())
			Expect(idsOf(mfs)).To(Equal([]string{"track-1", "track-2", "track-3"}))
		})

		It("combines the filters, as the listing does", func() {
			mfs, err := m.Tracks(ctx, librarymanager.Selection{Filter: map[string]any{"folder": "Artist", "format": "mp3", "missing_tag": "album_artist"}})
			Expect(err).ToNot(HaveOccurred())
			Expect(idsOf(mfs)).To(Equal([]string{"track-1"}))
		})

		It("takes a filter's JSON values (booleans, numbers, lists) as the listing's query string has them", func() {
			mfs, err := m.Tracks(ctx, librarymanager.Selection{Filter: map[string]any{
				"rip": false, "missing_tag": []any{"album_artist"}, "library_id": float64(1), "title": "one", "starred": false}})
			Expect(err).ToNot(HaveOccurred())
			Expect(idsOf(mfs)).To(Equal([]string{"track-1"}))
		})

		It("resolves the selected Tracks, in path order, without missing Files", func() {
			mfs, err := m.Tracks(ctx, librarymanager.Selection{IDs: []string{"track-4", "track-1", "track-5", "no-such-track"}})
			Expect(err).ToNot(HaveOccurred())
			Expect(idsOf(mfs)).To(Equal([]string{"track-1", "track-4"}))
		})

		It("refuses too many selected Tracks: those are selected by filter", func() {
			ids := make([]string, librarymanager.MaxSelected+1)
			for i := range ids {
				ids[i] = "track-1"
			}
			_, err := m.Tracks(ctx, librarymanager.Selection{IDs: ids})
			Expect(err).To(MatchError(librarymanager.ErrTooMany))
		})

		It("refuses a selection of neither Tracks nor a filter", func() {
			_, err := m.Tracks(ctx, librarymanager.Selection{})
			Expect(err).To(MatchError(librarymanager.ErrNoSelection))
		})
	})

	Describe("previewing a bulk edit", func() {
		rowsOf := func(p *librarymanager.Preview) map[string]librarymanager.PreviewRow {
			out := map[string]librarymanager.PreviewRow{}
			for _, r := range p.Rows {
				out[r.ID] = r
			}
			return out
		}

		It("shows each Track's old and new value, and what would happen to its File", func() {
			p, err := m.Preview(ctx, librarymanager.BulkEdit{
				Selection: librarymanager.Selection{Filter: map[string]any{"folder": "Artist"}},
				Field:     "albumartist", Values: []string{"The Artist"},
			}, 0, 100)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Total).To(Equal(3))
			Expect(p.Changes).To(Equal(1))
			Expect(p.Unchanged).To(Equal(1))
			Expect(p.Refused).To(Equal(map[string]int{"unsupportedFormat": 1}))
			rows := rowsOf(p)
			Expect(p.Rows).To(HaveLen(3))
			// track-1 has no album artist tag: its album artist only reads as its artist.
			Expect(rows["track-1"].Old).To(BeEmpty())
			Expect(rows["track-1"].New).To(Equal([]string{"The Artist"}))
			Expect(rows["track-1"].Status).To(Equal(librarymanager.StatusChange))
			Expect(rows["track-1"].Path).To(Equal("Artist/Album/01 One.mp3"))
			Expect(rows["track-2"].Old).To(Equal([]string{"The Artist"}))
			Expect(rows["track-2"].Status).To(Equal(librarymanager.StatusUnchanged))
			Expect(rows["track-3"].Status).To(Equal("unsupportedFormat"))
		})

		It("sets a multi-valued field as a list, and clears a field with no values", func() {
			p, err := m.Preview(ctx, librarymanager.BulkEdit{
				Selection: librarymanager.Selection{IDs: []string{"track-1"}},
				Field:     "genre", Values: []string{" Rock ", "Pop", ""},
			}, 0, 100)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Rows[0].Old).To(Equal([]string{"Rock"}))
			Expect(p.Rows[0].New).To(Equal([]string{"Rock", "Pop"}))

			p, err = m.Preview(ctx, librarymanager.BulkEdit{
				Selection: librarymanager.Selection{IDs: []string{"track-1", "track-4"}},
				Field:     "date",
			}, 0, 100)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Rows[0].Old).To(Equal([]string{"1999"}))
			Expect(p.Rows[0].New).To(BeEmpty())
			Expect(p.Rows[0].Status).To(Equal(librarymanager.StatusChange))
			Expect(p.Rows[1].Status).To(Equal(librarymanager.StatusUnchanged))
		})

		It("pages the rows, and counts them all", func() {
			p, err := m.Preview(ctx, librarymanager.BulkEdit{
				Selection: librarymanager.Selection{Filter: map[string]any{}},
				Field:     "comment", Values: []string{"checked"},
			}, 1, 2)
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Total).To(Equal(4))
			Expect(p.Changes).To(Equal(3))
			Expect(idsOfRows(p.Rows)).To(Equal([]string{"track-2", "track-3"}))
		})

		It("refuses a value the field can't hold, and a field that isn't edited in bulk", func() {
			_, err := m.Preview(ctx, librarymanager.BulkEdit{
				Selection: librarymanager.Selection{IDs: []string{"track-1"}}, Field: "date", Values: []string{"last year"},
			}, 0, 100)
			Expect(err).To(MatchError(tagedit.ErrInvalidTags))
			_, err = m.Preview(ctx, librarymanager.BulkEdit{
				Selection: librarymanager.Selection{IDs: []string{"track-1"}}, Field: "title", Values: []string{"Same"},
			}, 0, 100)
			Expect(err).To(MatchError(tagedit.ErrInvalidTags))
			_, err = m.Preview(ctx, librarymanager.BulkEdit{
				Selection: librarymanager.Selection{IDs: []string{"track-1"}}, Field: "album", Values: []string{"A", "B"},
			}, 0, 100)
			Expect(err).To(MatchError(tagedit.ErrInvalidTags))
		})
	})

	Describe("running a bulk edit", func() {
		runQueue := func(jobID string) *model.EditJob {
			GinkgoHelper()
			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			go func() { q.Run(runCtx); close(done) }()
			defer func() { cancel(); Eventually(done).Should(BeClosed()) }()
			job, err := q.Wait(ctx, jobID, 20*time.Second)
			Expect(err).ToNot(HaveOccurred())
			Expect(job.State).To(Equal(model.EditJobDone))
			return job
		}
		tagOf := func(trackPath, prop string) []string {
			GinkgoHelper()
			tags, err := taglib.ReadTags(filepath.Join(libDir, trackPath))
			Expect(err).ToNot(HaveOccurred())
			return tags[prop]
		}
		edit := librarymanager.BulkEdit{
			Selection: librarymanager.Selection{Filter: map[string]any{"folder": "Artist"}},
			Field:     "albumartist", Values: []string{"New Album Artist"},
		}

		It("runs as one job, with a tags step for each File that changes", func() {
			job, err := m.Run(ctx, edit)
			Expect(err).ToNot(HaveOccurred())
			Expect(job.Kind).To(Equal(model.EditTags))
			Expect(job.Total).To(Equal(2))
			queued, err := q.Job(ctx, job.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(queued.Steps).To(HaveLen(2))
			Expect(queued.Steps[0].MediaFileID).To(Equal("track-1"))
			Expect(queued.Steps[1].MediaFileID).To(Equal("track-2"))
			Expect(queued.Steps).To(HaveEach(HaveField("Kind", model.EditTags)))

			done := runQueue(job.ID)
			Expect(done.Done).To(Equal(2))
			Expect(done.Skipped).To(BeZero())
			Expect(tagOf("Artist/Album/01 One.mp3", "ALBUMARTIST")).To(Equal([]string{"New Album Artist"}))
			Expect(tagOf("Artist/Album/02 Two.mp3", "ALBUMARTIST")).To(Equal([]string{"New Album Artist"}))

			// One job in the Edit history: each of its writes says the job made two.
			history, total, err := editor.History(ctx, 0, 10)
			Expect(err).ToNot(HaveOccurred())
			Expect(total).To(BeEquivalentTo(2))
			Expect(history).To(HaveEach(HaveField("JobID", job.ID)))
			Expect(history).To(HaveEach(HaveField("JobSize", BeEquivalentTo(2))))
		})

		It("is undone as a whole, as one job", func() {
			before := tagOf("Artist/Album/01 One.mp3", "ALBUMARTIST")
			job, err := m.Run(ctx, edit)
			Expect(err).ToNot(HaveOccurred())
			runQueue(job.ID)
			scanned()

			steps, err := editjob.UndoJobSteps(ctx, ds, job.ID)
			Expect(err).ToNot(HaveOccurred())
			undo, err := q.Enqueue(ctx, model.EditUndo, steps)
			Expect(err).ToNot(HaveOccurred())
			done := runQueue(undo.ID)
			Expect(done.Done).To(Equal(2))
			Expect(done.Skipped).To(BeZero())
			Expect(tagOf("Artist/Album/01 One.mp3", "ALBUMARTIST")).To(Equal(before))
			Expect(tagOf("Artist/Album/02 Two.mp3", "ALBUMARTIST")).To(Equal(before))
		})

		It("clears a field in the Files", func() {
			job, err := m.Run(ctx, librarymanager.BulkEdit{
				Selection: librarymanager.Selection{IDs: []string{"track-1"}}, Field: "genre",
			})
			Expect(err).ToNot(HaveOccurred())
			runQueue(job.ID)
			Expect(tagOf("Artist/Album/01 One.mp3", "GENRE")).To(BeEmpty())
		})

		It("queues nothing when no File would change", func() {
			_, err := m.Run(ctx, librarymanager.BulkEdit{
				Selection: librarymanager.Selection{IDs: []string{"track-2", "track-3"}},
				Field:     "albumartist", Values: []string{"The Artist"},
			})
			Expect(err).To(MatchError(librarymanager.ErrNothingToChange))
			jobs, err := q.Jobs(ctx, 10)
			Expect(err).ToNot(HaveOccurred())
			Expect(jobs).To(BeEmpty())
		})
	})
})

func idsOfRows(rows []librarymanager.PreviewRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}
