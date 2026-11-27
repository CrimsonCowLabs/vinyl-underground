package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

var _ = Describe("Album Edit API", func() {
	var svc *fakeCorrections
	var ds *tests.MockDataStore
	var nativeRouter *Router
	var router http.Handler
	var adminToken, userToken string
	body := map[string]any{"albumId": "al-1", "name": "Record", "albumArtist": "Band", "year": 1999}

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds = &tests.MockDataStore{}
		auth.Init(ds)
		nativeRouter = New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil, nil, nil)
		svc = &fakeCorrections{}
		nativeRouter.corrections = svc
		router = server.JWTVerifier(nativeRouter)
		ds.Album(context.TODO()).(*tests.MockAlbumRepo).SetData(model.Albums{{ID: "al-1", Name: "Recrod", AlbumArtist: "Band"}})
		// Once written, the album's Tracks are on the album with its new ID.
		ds.MediaFile(context.TODO()).(*tests.MockMediaFileRepo).SetData(model.MediaFiles{
			{ID: "mf-1", AlbumID: "new-album"}, {ID: "mf-2", AlbumID: "new-album"}})

		adminUser := model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "adminpass"}
		regularUser := model.User{ID: "user-1", UserName: "regular", IsAdmin: false, NewPassword: "userpass"}
		Expect(ds.User(context.TODO()).Put(&adminUser)).To(Succeed())
		Expect(ds.User(context.TODO()).Put(&regularUser)).To(Succeed())
		var err error
		adminToken, err = auth.CreateToken(&adminUser)
		Expect(err).ToNot(HaveOccurred())
		userToken, err = auth.CreateToken(&regularUser)
		Expect(err).ToNot(HaveOccurred())
	})

	post := func(token string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		Expect(json.NewEncoder(&buf).Encode(body)).To(Succeed())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest("POST", "/correction/album", &buf, token))
		return w
	}
	type answer struct {
		JobID      string                `json:"jobId"`
		Done       bool                  `json:"done"`
		Background bool                  `json:"background"`
		AlbumID    string                `json:"albumId"`
		PickCover  corrections.PickCover `json:"pickCover"`
		Job        *model.EditJob        `json:"job"`
	}
	answerOf := func(w *httptest.ResponseRecorder) answer {
		GinkgoHelper()
		var got answer
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		return got
	}

	It("writes the album's values, and the Tracks' changed numbers, into its Files as one job, answering its new ID", func() {
		runEditQueue(nativeRouter, ds, fakeWriteKind{})
		svc.plan = fakePlan(corrections.JobAlbumEdit, "mf-1", "mf-2")
		w := post(adminToken, map[string]any{"albumId": "al-1", "name": "Record", "albumArtist": "Band", "year": 1999,
			"tracks": []map[string]any{{"id": "mf-2", "discNumber": 1, "trackNumber": 2}}})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(svc.plans).To(Equal([]string{"album al-1 Band|Record mf-2"}))
		got := answerOf(w)
		Expect(got.Done).To(BeTrue())
		Expect(got.AlbumID).To(Equal("new-album"))
		Expect(got.Job.Kind).To(Equal(corrections.JobAlbumEdit))
		Expect(got.Job.Total).To(Equal(2))
	})

	It("answers 202 while the job carries on in the background", func() {
		k := fakeWriteKind{hold: make(chan struct{})}
		runEditQueue(nativeRouter, ds, k)
		nativeRouter.editWait = 50 * time.Millisecond
		w := post(adminToken, body)
		Expect(w.Code).To(Equal(http.StatusAccepted))
		got := answerOf(w)
		Expect(got.Background).To(BeTrue())
		Expect(got.JobID).ToNot(BeEmpty())
		Expect(got.Done).To(BeFalse())
	})

	Describe("with a MusicBrainz release picked (#170)", func() {
		var search *fakeReleaseSearch
		picked := map[string]any{"albumId": "al-1", "name": "Record", "albumArtist": "Band", "year": 1999,
			"tracks":     []map[string]any{{"id": "mf-2", "discNumber": 1, "trackNumber": 2}},
			"mbzAlbumId": "8A7F5C4E-0d5b-4f1e-9d3c-3b2a1f0e9d8c", "canonical": map[string]any{"album": true, "titles": true},
			"replaceTrackIds": true, "cover": true, "genres": true}

		BeforeEach(func() {
			conf.Server.EnableExternalServices = true
			search = &fakeReleaseSearch{releases: []musicbrainz.Release{{ID: "8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c", Title: "Record"}}}
			nativeRouter.releaseSearch = search
		})

		It("looks the release up, and writes it with the dialog's values into the Files as one job", func() {
			runEditQueue(nativeRouter, ds, fakeWriteKind{})
			w := post(adminToken, picked)
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			Expect(search.looked).To(Equal([]string{"8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c"}))
			Expect(svc.plans).To(Equal([]string{"release al-1 8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c"}))
			Expect(svc.releases).To(HaveLen(1))
			rw := svc.releases[0]
			Expect(rw.Fix).To(PointTo(Equal(corrections.AlbumFix{Name: "Record", AlbumArtist: "Band", Year: 1999})))
			Expect(rw.Tracks).To(Equal([]corrections.TrackNumber{{ID: "mf-2", DiscNumber: 1, TrackNumber: 2}}))
			Expect(rw.Accept).To(Equal(corrections.Canonical{Album: true, Titles: true}))
			Expect(rw.ReplaceTrackIDs).To(BeTrue())
			Expect(rw.Cover).To(BeTrue())
			Expect(rw.Genres).To(BeTrue())
			got := answerOf(w)
			Expect(got.Done).To(BeTrue())
			Expect(got.Job.Kind).To(Equal(corrections.JobRelease))
			Expect(got.AlbumID).To(Equal("new-album"))
		})

		It("answers the album's new ID from a written Track, not from the cover written first", func() {
			runEditQueue(nativeRouter, ds, fakeWriteKind{})
			plan := fakePlan(corrections.JobRelease, "mf-1")
			plan.Steps = append([]model.EditJobStep{{Kind: fakeWrite, Path: "Band/Record/cover.jpg"}}, plan.Steps...)
			svc.plan = plan
			w := post(adminToken, picked)
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			Expect(answerOf(w).AlbumID).To(Equal("new-album"))
		})

		It("says when the job put the Files back, as one of them couldn't be written", func() {
			// The job's kind queued its rollback (see corrections.ReleaseJobKind).
			runEditQueue(nativeRouter, ds, fakeWriteKind{}, func(q *editjob.Queue) {
				q.Handle(corrections.JobRelease, rollbackKind{q: q})
			})
			svc.plan = fakePlan(corrections.JobRelease, "mf-1", "skip-2")
			w := post(adminToken, picked)
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			var got struct {
				Done          bool           `json:"done"`
				RolledBack    bool           `json:"rolledBack"`
				RollbackJobID string         `json:"rollbackJobId"`
				RollbackDone  bool           `json:"rollbackDone"`
				AlbumID       string         `json:"albumId"`
				Job           *model.EditJob `json:"job"`
			}
			Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
			Expect(got.RolledBack).To(BeTrue())
			Expect(got.RollbackJobID).ToNot(BeEmpty())
			Expect(got.RollbackDone).To(BeTrue())
			Expect(got.AlbumID).To(BeEmpty(), "the album keeps its ID")
			Expect(got.Job.Steps).To(ConsistOf(HaveField("MediaFileID", "skip-2")))
		})

		It("pairs the album's Tracks with the release before saving, writing nothing", func() {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, createAuthenticatedRequest("GET",
				"/musicbrainz/release/8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c/match?albumId=al-1", nil, adminToken))
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			Expect(svc.matched).To(Equal([]string{"al-1|8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c"}))
			var got corrections.ReleaseMatch
			Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
			Expect(got.Holds).To(BeTrue())
			Expect(got.Tracks).To(ConsistOf(HaveField("Paired", PointTo(HaveField("ID", "rt-1")))))
			Expect(svc.plans).To(BeEmpty())

			// An ID that is no MusicBrainz ID isn't asked for; one MusicBrainz doesn't have is notFound.
			w = httptest.NewRecorder()
			router.ServeHTTP(w, createAuthenticatedRequest("GET", "/musicbrainz/release/nope/match?albumId=al-1", nil, adminToken))
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			Expect(search.looked).To(HaveLen(1))
			search.err = musicbrainz.ErrNotFound
			w = httptest.NewRecorder()
			router.ServeHTTP(w, createAuthenticatedRequest("GET",
				"/musicbrainz/release/00000000-0000-4000-8000-000000000001/match?albumId=al-1", nil, adminToken))
			Expect(w.Code).To(Equal(http.StatusNotFound))
			search.err = nil
			w = httptest.NewRecorder()
			router.ServeHTTP(w, createAuthenticatedRequest("GET",
				"/musicbrainz/release/8a7f5c4e-0d5b-4f1e-9d3c-3b2a1f0e9d8c/match?albumId=al-1", nil, userToken))
			Expect(w.Code).To(Equal(http.StatusForbidden))
		})

		It("answers why MusicBrainz couldn't be asked, writing nothing", func() {
			runEditQueue(nativeRouter, ds, fakeWriteKind{})
			search.err = musicbrainz.ErrBusy
			w := post(adminToken, picked)
			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
			Expect(w.Body.String()).To(ContainSubstring(`"busy"`))

			conf.Server.EnableExternalServices = false
			w = post(adminToken, picked)
			Expect(w.Body.String()).To(ContainSubstring(`"disabled"`))
			Expect(svc.plans).To(BeEmpty())
		})

		It("answers editingOff with no edit job queue, asking nothing", func() {
			w := post(adminToken, picked)
			Expect(w.Code).To(Equal(http.StatusForbidden))
			Expect(search.looked).To(BeEmpty())
			Expect(svc.plans).To(BeEmpty())
		})
	})

	It("refuses a save that changes nothing", func() {
		runEditQueue(nativeRouter, ds, fakeWriteKind{})
		svc.planErr = corrections.ErrNothingToWrite
		w := post(adminToken, body)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		var res struct{ Error string }
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Error).To(Equal("invalid"))
	})

	It("answers editingOff with no edit job queue", func() {
		w := post(adminToken, body)
		Expect(w.Code).To(Equal(http.StatusForbidden))
		var res struct{ Error string }
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		Expect(res.Error).To(Equal("editingOff"))
	})

	It("denies access to regular users", func() {
		Expect(post(userToken, body).Code).To(Equal(http.StatusForbidden))
		Expect(svc.plans).To(BeEmpty())
	})
})

// rollbackKind stands in for corrections.ReleaseJobKind: a job with a File skipped queues the
// undo of those written, and names it in its Args.
type rollbackKind struct{ q *editjob.Queue }

func (rollbackKind) Start(context.Context, *model.EditJob) error { return nil }

func (k rollbackKind) Done(ctx context.Context, job *model.EditJob, steps model.EditJobSteps) error {
	var written []model.EditJobStep
	for _, s := range steps {
		if s.State == model.EditStepWritten {
			written = append(written, model.EditJobStep{Kind: fakeWrite, MediaFileID: s.MediaFileID, Path: s.Path})
		}
	}
	if len(written) == len(steps) {
		return nil
	}
	u, err := k.q.EnqueueWith(ctx, model.EditUndo, editjob.UndoJobArgs(job.ID), written)
	if err != nil {
		return err
	}
	job.Args = `{"rollback":"` + u.ID + `"}`
	return nil
}

func (rollbackKind) Undone(context.Context, *model.EditJob, model.EditJobSteps) error { return nil }
