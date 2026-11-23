package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("/api/editHistory", func() {
	var (
		router http.Handler
		api    *Router
		queue  *editjob.Queue
		// runQueue starts the edit job queue, until the test ends.
		runQueue        func()
		admin, listener model.User
		path            string
		mfs             *tests.MockMediaFileRepo
	)

	// scanned records the File's size and modification time on its Track, as a scan does.
	scanned := func() {
		GinkgoHelper()
		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		mfs.SetData(model.MediaFiles{{ID: "t1", LibraryID: 1, Path: "01 Song.mp3", Title: "Song", Artist: "The Band",
			Size: info.Size(), UpdatedAt: info.ModTime()}})
	}

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds := &tests.MockDataStore{}
		auth.Init(ds)

		libDir := GinkgoT().TempDir()
		path = filepath.Join(libDir, "01 Song.mp3")
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(path, data, 0o644)).To(Succeed())
		ds.MockedLibrary = &tests.MockLibraryRepo{}
		ds.MockedLibrary.(*tests.MockLibraryRepo).SetData(model.Libraries{{ID: 1, Name: "Music", Path: libDir}})
		mfs = tests.CreateMockMediaFileRepo()
		ds.MockedMediaFile = mfs
		ds.MockedEditJournal = &tests.MockEditJournalRepo{}
		scanned()

		editor := tagedit.New(ds, nil)
		api = New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		// Edits and undos run as jobs (#162).
		queue = editjob.New(ds, nil, editjob.Kinds(ds, editor))
		api.SetEditQueue(queue)
		runQueue = func() {
			qctx, stop := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { queue.Run(qctx); close(done) }()
			DeferCleanup(func() { stop(); Eventually(done).Should(BeClosed()) })
		}
		runQueue()
		router = server.JWTVerifier(api)
		admin = model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pass"}
		listener = model.User{ID: "user-1", UserName: "listener", NewPassword: "pass"}
		Expect(ds.User(context.TODO()).Put(&admin)).To(Succeed())
		Expect(ds.User(context.TODO()).Put(&listener)).To(Succeed())

		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "Music", Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})
	})

	do := func(user model.User, method, url string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		token, err := auth.CreateToken(&user)
		Expect(err).ToNot(HaveOccurred())
		var b []byte
		if body != nil {
			b, err = json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest(method, url, bytes.NewBuffer(b), token))
		return w
	}
	edit := func(title string) {
		GinkgoHelper()
		w := do(admin, http.MethodPut, "/trackTags/t1", map[string]any{"values": map[string][]string{"title": {title}}})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		scanned()
	}
	history := func() []tagedit.HistoryEntry {
		GinkgoHelper()
		w := do(admin, http.MethodGet, "/editHistory?_start=0&_end=25", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var got []tagedit.HistoryEntry
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(w.Header().Get("X-Total-Count")).To(Equal(strconv.Itoa(len(got))))
		return got
	}
	errorCode := func(w *httptest.ResponseRecorder) string {
		GinkgoHelper()
		var got map[string]string
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		return got["error"]
	}

	It("lists every write, newest first, with its File, Track and each field's old and new values", func() {
		edit("Song (Live)")
		edit("Song (Remix)")

		got := history()

		Expect(got).To(HaveLen(2))
		Expect(got[0].Path).To(Equal("01 Song.mp3"))
		Expect(got[0].Track).ToNot(BeNil())
		Expect(got[0].Track.ID).To(Equal("t1"))
		Expect(got[0].Track.Title).To(Equal("Song"))
		Expect(got[0].Track.Artist).To(Equal("The Band"))
		Expect(got[0].Changes).To(Equal([]tagedit.TagChange{{Tag: "TITLE", Old: []string{"Song (Live)"}, New: []string{"Song (Remix)"}}}))
		Expect(got[1].Changes).To(Equal([]tagedit.TagChange{{Tag: "TITLE", Old: []string{"Song"}, New: []string{"Song (Live)"}}}))
		Expect(got[0].CreatedAt).ToNot(BeZero())
		Expect(got[0].State).To(Equal(model.EditWritten))
	})

	It("pages the list", func() {
		edit("One")
		edit("Two")
		w := do(admin, http.MethodGet, "/editHistory?_start=1&_end=2", nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(w.Header().Get("X-Total-Count")).To(Equal("2"))
		var got []tagedit.HistoryEntry
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got).To(HaveLen(1))
		Expect(got[0].Changes[0].New).To(Equal([]string{"One"}))
	})

	It("undoes an entry, and the undo shows in the list and can be undone", func() {
		edit("Song (Live)")
		entry := history()[0].ID

		w := do(admin, http.MethodPost, "/editHistory/"+entry+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		scanned()

		got := history()
		Expect(got).To(HaveLen(2))
		Expect(got[0].UndoOf).To(Equal(entry))
		Expect(got[0].Changes[0].New).To(Equal([]string{"Song"}))

		w = do(admin, http.MethodPost, "/editHistory/"+got[0].ID+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(do(admin, http.MethodGet, "/trackTags/t1", nil).Body.String()).To(ContainSubstring("Song (Live)"))
		scanned()
		got = history()
		Expect(got).To(HaveLen(3))
		Expect(got[0].Redo).To(BeTrue(), "an undo of an undo is a redo")
		Expect(got[1].Redo).To(BeFalse())
	})

	It("refuses an undo a later edit overlaps", func() {
		edit("Song (Live)")
		entry := history()[0].ID
		edit("Song (Remix)")

		w := do(admin, http.MethodPost, "/editHistory/"+entry+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusConflict))
		Expect(errorCode(w)).To(Equal("changedLater"))
	})

	It("refuses to undo an entry twice", func() {
		edit("Song (Live)")
		entry := history()[0].ID
		Expect(do(admin, http.MethodPost, "/editHistory/"+entry+"/undo", nil).Code).To(Equal(http.StatusOK))
		scanned()

		w := do(admin, http.MethodPost, "/editHistory/"+entry+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusConflict))
		Expect(errorCode(w)).To(Equal("alreadyUndone"))
	})

	It("refuses an undo of a File that changed on disk", func() {
		edit("Song (Live)")
		entry := history()[0].ID
		later := time.Now().Add(time.Hour)
		Expect(os.Chtimes(path, later, later)).To(Succeed())

		w := do(admin, http.MethodPost, "/editHistory/"+entry+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusConflict))
		Expect(errorCode(w)).To(Equal("changedOnDisk"))
	})

	It("undoes an entry as a job of its own, journaled with the job's ID", func() {
		edit("Song (Live)")
		entry := history()[0]
		w := do(admin, http.MethodPost, "/editHistory/"+entry.ID+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var res struct {
			JobID string `json:"jobId"`
			Entry string `json:"entryId"`
		}
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.JobID).ToNot(BeEmpty())
		Expect(res.JobID).ToNot(Equal(entry.JobID))
		scanned()
		Expect(history()[0].JobID).To(Equal(res.JobID))
		job, err := queue.Job(context.Background(), res.JobID)
		Expect(err).ToNot(HaveOccurred())
		Expect(job.Kind).To(Equal(model.EditUndo))
	})

	It("answers that an undo runs in the background when its job doesn't end soon", func() {
		edit("Song (Live)")
		entry := history()[0].ID
		// A queue that is busy, or stopped: here, a router whose queue doesn't run.
		idle := editjob.New(api.ds, nil, editjob.Kinds(api.ds, api.tagEditor))
		api.SetEditQueue(idle)
		api.editWait = 50 * time.Millisecond
		w := do(admin, http.MethodPost, "/editHistory/"+entry+"/undo", nil)
		Expect(w.Code).To(Equal(http.StatusAccepted), w.Body.String())
		Expect(w.Body.String()).To(ContainSubstring(`"background":true`))
	})

	It("says, of each entry, its job and how many Files the job wrote", func() {
		journal := api.ds.EditJournal(context.Background())
		for _, p := range []string{"a.mp3", "b.mp3"} {
			Expect(journal.Put(&model.EditEntry{JobID: "batch", Kind: model.EditTags, State: model.EditWritten,
				LibraryID: 1, Path: p, NewPath: p})).To(Succeed())
		}
		edit("Song (Live)")

		got := history()
		Expect(got).To(HaveLen(3))
		Expect(got[0].JobSize).To(Equal(1))
		Expect(got[1].JobID).To(Equal("batch"))
		Expect(got[1].JobSize).To(Equal(2))
		Expect(got[2].JobSize).To(Equal(2))
	})

	Describe("undoing a whole job", func() {
		It("undoes every File of the job, as a job of its own, and answers its outcome", func() {
			edit("Song (Live)")
			entry := history()[0]

			w := do(admin, http.MethodPost, "/editJob/"+entry.JobID+"/undo", nil)
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			var job model.EditJob
			Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
			Expect(job.ID).ToNot(Equal(entry.JobID))
			Expect(job.Kind).To(Equal(model.EditUndo))
			Expect(job.State).To(Equal(model.EditJobDone))
			Expect(job.Total).To(Equal(1))
			Expect(job.Skipped).To(BeZero())
			Expect(do(admin, http.MethodGet, "/trackTags/t1", nil).Body.String()).ToNot(ContainSubstring("Song (Live)"))
			scanned()
			Expect(history()[0].UndoOf).To(Equal(entry.ID))
		})

		It("lists the Files it skipped, and why", func() {
			edit("Song (Live)")
			jobID := history()[0].JobID
			edit("Song (Remix)")

			w := do(admin, http.MethodPost, "/editJob/"+jobID+"/undo", nil)
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			var job model.EditJob
			Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
			Expect(job.Skipped).To(Equal(1))
			Expect(job.Steps).To(HaveLen(1))
			Expect(job.Steps[0].Code).To(Equal("changedLater"))
		})

		It("answers 202 while the undo job runs in the background", func() {
			edit("Song (Live)")
			jobID := history()[0].JobID
			api.SetEditQueue(editjob.New(api.ds, nil, editjob.Kinds(api.ds, api.tagEditor)))
			api.editWait = 50 * time.Millisecond
			w := do(admin, http.MethodPost, "/editJob/"+jobID+"/undo", nil)
			Expect(w.Code).To(Equal(http.StatusAccepted), w.Body.String())
		})

		It("answers notFound for a job that isn't there, and is admin only", func() {
			Expect(do(admin, http.MethodPost, "/editJob/nope/undo", nil).Code).To(Equal(http.StatusNotFound))
			edit("Song (Live)")
			jobID := history()[0].JobID
			Expect(do(listener, http.MethodPost, "/editJob/"+jobID+"/undo", nil).Code).To(Equal(http.StatusForbidden))
		})
	})

	It("answers notFound for an entry that isn't there", func() {
		w := do(admin, http.MethodPost, "/editHistory/nope/undo", nil)
		Expect(w.Code).To(Equal(http.StatusNotFound))
	})

	It("is admin only", func() {
		edit("Song (Live)")
		entry := history()[0].ID
		Expect(do(listener, http.MethodGet, "/editHistory", nil).Code).To(Equal(http.StatusForbidden))
		Expect(do(listener, http.MethodPost, "/editHistory/"+entry+"/undo", nil).Code).To(Equal(http.StatusForbidden))
	})
})
