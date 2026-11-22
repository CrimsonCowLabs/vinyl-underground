package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/corrections"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeCorrections struct {
	all     model.Corrections
	err     error
	renamed []string
	merged  []string
	undone  []string
	covers  []string
	blocks  []string
}

func (f *fakeCorrections) GetAll(context.Context) (model.Corrections, error) { return f.all, f.err }

func (f *fakeCorrections) RenameArtist(_ context.Context, artistID, name string) (*corrections.Applied, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.renamed = append(f.renamed, artistID+"->"+name)
	return &corrections.Applied{Correction: model.Correction{ID: "c1", To: name}, ArtistID: "new-id", Done: true}, nil
}

func (f *fakeCorrections) ChooseAlbumCover(_ context.Context, albumID string, cover corrections.AlbumCover) (*corrections.Applied, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.covers = append(f.covers, albumID+"->"+cover.Hash+" "+cover.Source)
	return &corrections.Applied{Correction: model.Correction{ID: "c2", To: cover.Source}, Done: true}, nil
}

func (f *fakeCorrections) BlockAlbumCover(_ context.Context, albumID, imageHash string) (*corrections.Applied, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.blocks = append(f.blocks, albumID+"-x"+imageHash)
	return &corrections.Applied{Correction: model.Correction{ID: "c3"}, Done: true}, nil
}

func (f *fakeCorrections) MergeArtist(_ context.Context, artistID, intoID string) (*corrections.Applied, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.merged = append(f.merged, artistID+"->"+intoID)
	return &corrections.Applied{Correction: model.Correction{ID: "c2"}, ArtistID: intoID, Done: true}, nil
}

func (f *fakeCorrections) Undo(_ context.Context, id string) (*corrections.Applied, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.undone = append(f.undone, id)
	return &corrections.Applied{Correction: model.Correction{ID: id}, ArtistID: "old-id", Done: false}, nil
}

var _ = Describe("Corrections API", func() {
	var svc *fakeCorrections
	var router http.Handler
	var adminToken, userToken string

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.EnableSharing = false
		ds := &tests.MockDataStore{}
		auth.Init(ds)
		nativeRouter := New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(), nil, nil, nil, nil, nil, nil, nil)
		svc = &fakeCorrections{}
		nativeRouter.corrections = svc
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

	It("renames an Artist", func() {
		w := do("POST", "/correction/artistRename", adminToken, map[string]string{"artistId": "ar-1", "name": "Grateful Dead"})
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(svc.renamed).To(Equal([]string{"ar-1->Grateful Dead"}))
		var got map[string]any
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got).To(HaveKeyWithValue("artistId", "new-id"))
		Expect(got).To(HaveKeyWithValue("done", true))
	})

	It("merges an Artist into another", func() {
		w := do("POST", "/correction/artistMerge", adminToken, map[string]string{"artistId": "ar-1", "intoId": "ar-2"})
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(svc.merged).To(Equal([]string{"ar-1->ar-2"}))
		var got map[string]any
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got).To(HaveKeyWithValue("artistId", "ar-2"))
	})

	It("refuses an undo a later Correction builds on", func() {
		svc.err = corrections.ErrUndoLaterFirst
		w := do("POST", "/correction/c1/undo", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(errorCode(w)).To(Equal("undoLaterFirst"))
	})

	It("undoes a Correction", func() {
		w := do("POST", "/correction/c1/undo", adminToken, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(svc.undone).To(Equal([]string{"c1"}))
	})

	DescribeTable("answers each failure with a status and a code the UI can show",
		func(err error, status int, code string) {
			svc.err = err
			w := do("POST", "/correction/artistRename", adminToken, map[string]string{"artistId": "ar-1", "name": "X"})
			Expect(w.Code).To(Equal(status))
			Expect(errorCode(w)).To(Equal(code))
		},
		Entry("existing Artist", corrections.ErrArtistExists, http.StatusConflict, "artistExists"),
		Entry("renamed away", corrections.ErrRenamedAway, http.StatusBadRequest, "renamedAway"),
		Entry("already corrected", model.ErrAlreadyExists, http.StatusConflict, "alreadyCorrected"),
		Entry("invalid name", model.ErrValidation, http.StatusBadRequest, "invalid"),
		Entry("unknown Artist", model.ErrNotFound, http.StatusNotFound, "notFound"),
	)

	It("denies access to regular users", func() {
		Expect(do("GET", "/correction", userToken, nil).Code).To(Equal(http.StatusForbidden))
		Expect(do("POST", "/correction/artistRename", userToken, map[string]string{"artistId": "ar-1", "name": "X"}).Code).
			To(Equal(http.StatusForbidden))
		Expect(do("POST", "/correction/artistMerge", userToken, map[string]string{"artistId": "ar-1", "intoId": "ar-2"}).Code).
			To(Equal(http.StatusForbidden))
		Expect(do("POST", "/correction/c1/undo", userToken, nil).Code).To(Equal(http.StatusForbidden))
		Expect(svc.renamed).To(BeEmpty())
		Expect(svc.merged).To(BeEmpty())
		Expect(svc.undone).To(BeEmpty())
	})
})
