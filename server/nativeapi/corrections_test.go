package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeCorrections struct {
	all    model.Corrections
	err    error
	undone []string
	blocks []string
	// releases are the releases planned (see PlanRelease), with what was asked; matched those
	// paired.
	releases []corrections.ReleaseWrite
	matched  []string
	// plans are the jobs planned (see PlanAlbumEdit), plan what each answers (nil for one of
	// "mf-1"), planErr its refusal.
	plans   []string
	plan    *corrections.Plan
	planErr error
}

func (f *fakeCorrections) GetAll(context.Context) (model.Corrections, error) { return f.all, f.err }

func (f *fakeCorrections) BlockAlbumCover(_ context.Context, albumID, imageHash string) (*corrections.Applied, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.blocks = append(f.blocks, albumID+"-x"+imageHash)
	return &corrections.Applied{Correction: model.Correction{ID: "c3"}, Done: true}, nil
}

func (f *fakeCorrections) MatchRelease(_ context.Context, albumID string, rel *musicbrainz.Release) (*corrections.ReleaseMatch, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.matched = append(f.matched, albumID+"|"+rel.ID)
	return &corrections.ReleaseMatch{Release: corrections.ReleaseSummary{ID: rel.ID}, Holds: true,
		Tracks: []corrections.PairedTrack{{ID: "mf-1", Paired: &corrections.ReleaseTrack{ID: "rt-1"}}}}, nil
}

func (f *fakeCorrections) PlanRelease(_ context.Context, _ *tagedit.Editor, albumID string, rel *musicbrainz.Release, w corrections.ReleaseWrite) (*corrections.Plan, error) {
	f.releases = append(f.releases, w)
	plan, err := f.planned(fmt.Sprintf("release %s %s", albumID, rel.ID))
	if plan != nil && f.plan == nil {
		plan.Kind = corrections.JobRelease
	}
	return plan, err
}

func (f *fakeCorrections) Undo(_ context.Context, id string) (*corrections.Applied, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.undone = append(f.undone, id)
	return &corrections.Applied{Correction: model.Correction{ID: id}, Done: true}, nil
}

func (f *fakeCorrections) PlanMigration(context.Context, *tagedit.Editor) (*corrections.Migration, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.plans = append(f.plans, "migration")
	return &corrections.Migration{Items: []corrections.MigrationItem{{Correction: model.Correction{ID: "c1",
		Kind: model.CorrectionArtistRename}, Files: 2, Changes: []corrections.MigrationChange{{Field: "artist", From: "A", To: "B"}}}},
		Files: 2}, nil
}

func (f *fakeCorrections) MigrationJob(context.Context, *tagedit.Editor) (*corrections.Plan, error) {
	return f.planned("migrate")
}

var _ = Describe("Corrections API", func() {
	var svc *fakeCorrections
	var router http.Handler
	var nativeRouter *Router
	var adminToken, userToken string

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds := &tests.MockDataStore{}
		auth.Init(ds)
		nativeRouter = New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil, nil, nil)
		svc = &fakeCorrections{}
		nativeRouter.corrections = svc
		runEditQueue(nativeRouter, ds, fakeWriteKind{})
		router = server.JWTVerifier(nativeRouter)

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

	do := func(method, path, token string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			Expect(json.NewEncoder(&buf).Encode(body)).To(Succeed())
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest(method, path, &buf, token))
		return w
	}
	errorCode := func(w *httptest.ResponseRecorder) string {
		var res struct{ Error string }
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		return res.Error
	}

	It("lists the Corrections with a total count", func() {
		svc.all = model.Corrections{{ID: "c1", Kind: model.CorrectionArtistRename, From: "Greatful Dead", To: "Grateful Dead", TrackCount: 0}}
		w := do("GET", "/correction", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(w.Header().Get("X-Total-Count")).To(Equal("1"))
		var got []map[string]any
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got).To(HaveLen(1))
		Expect(got[0]).To(HaveKeyWithValue("from", "Greatful Dead"))
		Expect(got[0]).To(HaveKeyWithValue("trackCount", BeNumerically("==", 0)))
	})

	It("renames an Artist in its Files, as one job, saving no Correction", func() {
		svc.plan = fakePlan(corrections.JobArtistRename, "mf-1", "mf-2")
		w := do("POST", "/correction/artistRename", adminToken, map[string]string{"artistId": "ar-1", "name": "Grateful Dead"})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(svc.plans).To(Equal([]string{"rename ar-1->Grateful Dead"}))
		var got struct {
			JobID    string         `json:"jobId"`
			Done     bool           `json:"done"`
			ArtistID string         `json:"artistId"`
			Job      *model.EditJob `json:"job"`
		}
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got.Done).To(BeTrue())
		Expect(got.ArtistID).To(Equal("new-artist"))
		Expect(got.Job.Kind).To(Equal(corrections.JobArtistRename))
		Expect(got.Job.Total).To(Equal(2))
		Expect(got.Job.Skipped).To(BeZero())
	})

	It("merges an Artist into another in its Files, and lists the Files it skipped", func() {
		svc.plan = fakePlan(corrections.JobArtistMerge, "mf-1", "skip-2")
		w := do("POST", "/correction/artistMerge", adminToken, map[string]string{"artistId": "ar-1", "intoId": "ar-2"})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(svc.plans).To(Equal([]string{"merge ar-1->ar-2"}))
		var got struct {
			Job *model.EditJob `json:"job"`
		}
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got.Job.Skipped).To(Equal(1))
		Expect(got.Job.Steps).To(HaveLen(1))
		Expect(got.Job.Steps[0].Code).To(Equal("changedOnDisk"))
	})

	It("refuses to undo a Correction the migration writes into the Files", func() {
		svc.err = corrections.ErrMigrateFirst
		w := do("POST", "/correction/c1/undo", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(errorCode(w)).To(Equal("migrateFirst"))
	})

	Describe("the Correction migration", func() {
		BeforeEach(func() {
			nativeRouter.tagEditor = tagedit.New(&tests.MockDataStore{}, nil)
		})

		It("lists the Corrections it writes, writing nothing", func() {
			w := do("GET", "/correction/migration", adminToken, nil)
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			var got corrections.Migration
			Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
			Expect(got.Files).To(Equal(2))
			Expect(got.Items).To(HaveLen(1))
			Expect(got.Items[0].Changes).To(Equal([]corrections.MigrationChange{{Field: "artist", From: "A", To: "B"}}))
		})

		It("runs it as one job when approved, answering the Files it skipped", func() {
			svc.plan = fakePlan(corrections.JobMigrate, "mf-1", "skip-2")
			w := do("POST", "/correction/migration", adminToken, nil)
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			Expect(svc.plans).To(Equal([]string{"migrate"}))
			var got struct {
				Job *model.EditJob `json:"job"`
			}
			Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
			Expect(got.Job.Kind).To(Equal(corrections.JobMigrate))
			Expect(got.Job.Skipped).To(Equal(1))
		})

		It("answers done when there is nothing to write", func() {
			svc.planErr = corrections.ErrNothingToWrite
			w := do("POST", "/correction/migration", adminToken, nil)
			Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
			Expect(w.Body.String()).To(ContainSubstring(`"done":true`))
		})

		It("answers editingOff with no edit job queue", func() {
			nativeRouter.SetEditQueue(nil)
			w := do("POST", "/correction/migration", adminToken, nil)
			Expect(w.Code).To(Equal(http.StatusForbidden))
			Expect(errorCode(w)).To(Equal("editingOff"))
		})

		It("denies access to regular users", func() {
			Expect(do("GET", "/correction/migration", userToken, nil).Code).To(Equal(http.StatusForbidden))
			Expect(do("POST", "/correction/migration", userToken, nil).Code).To(Equal(http.StatusForbidden))
			Expect(svc.plans).To(BeEmpty())
		})
	})

	It("undoes a Correction", func() {
		w := do("POST", "/correction/c1/undo", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(svc.undone).To(Equal([]string{"c1"}))
	})

	DescribeTable("answers each failure with a status and a code the UI can show",
		func(err error, status int, code string) {
			svc.planErr = err
			w := do("POST", "/correction/artistRename", adminToken, map[string]string{"artistId": "ar-1", "name": "X"})
			Expect(w.Code).To(Equal(status))
			Expect(errorCode(w)).To(Equal(code))
		},
		Entry("existing Artist", corrections.ErrArtistExists, http.StatusConflict, "artistExists"),
		Entry("renamed away", corrections.ErrRenamedAway, http.StatusBadRequest, "renamedAway"),
		Entry("invalid name", model.ErrValidation, http.StatusBadRequest, "invalid"),
		Entry("unknown Artist", model.ErrNotFound, http.StatusNotFound, "notFound"),
		Entry("a library editing is off for", tagedit.ErrEditingOff, http.StatusForbidden, "editingOff"),
		Entry("a File that can't be edited", tagedit.ErrUnsupportedFormat, http.StatusUnprocessableEntity, "unsupportedFormat"),
	)

	It("answers editingOff with no edit job queue (editing off at startup)", func() {
		nativeRouter.SetEditQueue(nil)
		w := do("POST", "/correction/artistMerge", adminToken, map[string]string{"artistId": "ar-1", "intoId": "ar-2"})
		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(errorCode(w)).To(Equal("editingOff"))
		Expect(svc.plans).To(BeEmpty())
	})

	It("denies access to regular users", func() {
		Expect(do("GET", "/correction", userToken, nil).Code).To(Equal(http.StatusForbidden))
		Expect(do("POST", "/correction/artistRename", userToken, map[string]string{"artistId": "ar-1", "name": "X"}).Code).
			To(Equal(http.StatusForbidden))
		Expect(do("POST", "/correction/artistMerge", userToken, map[string]string{"artistId": "ar-1", "intoId": "ar-2"}).Code).
			To(Equal(http.StatusForbidden))
		Expect(do("POST", "/correction/c1/undo", userToken, nil).Code).To(Equal(http.StatusForbidden))
		Expect(svc.plans).To(BeEmpty())
		Expect(svc.undone).To(BeEmpty())
	})
})
