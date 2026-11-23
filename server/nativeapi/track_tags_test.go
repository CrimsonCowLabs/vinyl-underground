package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("/api/trackTags", func() {
	var (
		router          http.Handler
		admin, listener model.User
		path            string
	)

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
		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		ds.MockedLibrary = &tests.MockLibraryRepo{}
		ds.MockedLibrary.(*tests.MockLibraryRepo).SetData(model.Libraries{{ID: 1, Name: "Music", Path: libDir}})
		mfs := tests.CreateMockMediaFileRepo()
		mfs.SetData(model.MediaFiles{{ID: "t1", LibraryID: 1, Path: "01 Song.mp3", Title: "Song", Size: info.Size(), UpdatedAt: info.ModTime()}})
		ds.MockedMediaFile = mfs

		router = server.JWTVerifier(New(ds, nil, nil, nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, tagedit.New(ds, nil)))
		admin = model.User{ID: "admin-1", UserName: "admin", IsAdmin: true, NewPassword: "pass"}
		listener = model.User{ID: "user-1", UserName: "listener", NewPassword: "pass"}
		Expect(ds.User(context.TODO()).Put(&admin)).To(Succeed())
		Expect(ds.User(context.TODO()).Put(&listener)).To(Succeed())

		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "Music", Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})
	})

	do := func(user model.User, method string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		token, err := auth.CreateToken(&user)
		Expect(err).ToNot(HaveOccurred())
		var b []byte
		if body != nil {
			b, err = json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, createAuthenticatedRequest(method, "/trackTags/t1", bytes.NewBuffer(b), token))
		return w
	}
	errorCode := func(w *httptest.ResponseRecorder) string {
		GinkgoHelper()
		var got map[string]string
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		return got["error"]
	}

	It("shows the admin the Track's tags from its File", func() {
		w := do(admin, http.MethodGet, nil)
		Expect(w.Code).To(Equal(http.StatusOK))
		var got tagedit.TrackTags
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got.Values).To(HaveKeyWithValue("title", []string{"Song"}))
		Expect(got.Fields).ToNot(BeEmpty())
	})

	It("writes the admin's edit into the File", func() {
		w := do(admin, http.MethodPut, map[string]any{"values": map[string][]string{"title": {"Song (Live)"}}})
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var got tagedit.Result
		Expect(json.Unmarshal(w.Body.Bytes(), &got)).To(Succeed())
		Expect(got.Changed).To(BeTrue())
		w = do(admin, http.MethodGet, nil)
		Expect(w.Body.String()).To(ContainSubstring("Song (Live)"))
	})

	It("refuses a File that changed on disk", func() {
		later := time.Now().Add(time.Hour)
		Expect(os.Chtimes(path, later, later)).To(Succeed())
		w := do(admin, http.MethodPut, map[string]any{"values": map[string][]string{"title": {"x"}}})
		Expect(w.Code).To(Equal(http.StatusConflict))
		Expect(errorCode(w)).To(Equal("changedOnDisk"))
	})

	It("refuses an invalid value", func() {
		w := do(admin, http.MethodPut, map[string]any{"values": map[string][]string{"tracknumber": {"seven"}}})
		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(errorCode(w)).To(Equal("invalid"))
	})

	It("is off while editing is off", func() {
		core.SetLibraryEditing(core.LibraryEditing{Libraries: []core.LibraryWritable{{ID: 1, Name: "Music"}}})
		Expect(errorCode(do(admin, http.MethodGet, nil))).To(Equal("editingOff"))
		w := do(admin, http.MethodPut, map[string]any{"values": map[string][]string{"title": {"x"}}})
		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(errorCode(w)).To(Equal("editingOff"))
	})

	It("is admin only", func() {
		Expect(do(listener, http.MethodGet, nil).Code).To(Equal(http.StatusForbidden))
		w := do(listener, http.MethodPut, map[string]any{"values": map[string][]string{"title": {"x"}}})
		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(do(admin, http.MethodGet, nil).Body.String()).ToNot(ContainSubstring(`"x"`))
	})
})
