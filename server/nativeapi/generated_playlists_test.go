package nativeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/generatedplaylist"
	"github.com/navidrome/navidrome/core/llm"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeGenerator struct {
	enabled     bool
	err         error
	gotPrompt   string
	gotLength   int
	gotName     string
	gotTrackIDs []string
	gotUser     string
	gotID       string
}

func (f *fakeGenerator) Enabled() bool { return f.enabled }

func (f *fakeGenerator) Preview(ctx context.Context, prompt string, length int) (*generatedplaylist.Preview, error) {
	f.gotPrompt, f.gotLength = prompt, length
	if f.err != nil {
		return nil, f.err
	}
	return &generatedplaylist.Preview{Prompt: prompt, Length: length, Name: "Rainy Sunday", Tracks: model.MediaFiles{
		{ID: "mf-1", Title: "Cut Your Hair", Artist: "Pavement"},
	}}, nil
}

func (f *fakeGenerator) Save(ctx context.Context, name, prompt string, length int, trackIDs []string) (string, error) {
	f.gotName, f.gotPrompt, f.gotLength, f.gotTrackIDs = name, prompt, length, trackIDs
	usr, _ := request.UserFrom(ctx)
	f.gotUser = usr.ID
	if f.err != nil {
		return "", f.err
	}
	return "pls-1", nil
}

func (f *fakeGenerator) Regenerate(ctx context.Context, playlistID string) (*generatedplaylist.Preview, error) {
	f.gotID = playlistID
	if f.err != nil {
		return nil, f.err
	}
	return &generatedplaylist.Preview{Prompt: "rainy Sunday 90s indie", Length: 12, Name: "Rainy Sunday", Tracks: model.MediaFiles{
		{ID: "mf-3", Title: "Alison", Artist: "Slowdive"},
	}}, nil
}

func (f *fakeGenerator) Replace(ctx context.Context, playlistID string, trackIDs []string) error {
	f.gotID, f.gotTrackIDs = playlistID, trackIDs
	usr, _ := request.UserFrom(ctx)
	f.gotUser = usr.ID
	return f.err
}

var _ = Describe("Generated Playlist endpoints", func() {
	var gen *fakeGenerator
	alice := model.User{ID: "u1", UserName: "alice"}

	BeforeEach(func() {
		gen = &fakeGenerator{enabled: true}
	})

	call := func(handler http.HandlerFunc, body string) (int, map[string]any) {
		r := httptest.NewRequest(http.MethodPost, "/generatedPlaylist", strings.NewReader(body))
		r = r.WithContext(request.WithUser(r.Context(), alice))
		w := httptest.NewRecorder()
		handler(w, r)
		var res map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		return w.Code, res
	}

	// callPlaylist calls handler for the Generated Playlist id, as the router would.
	callPlaylist := func(handler http.HandlerFunc, id, body string) (int, map[string]any) {
		r := httptest.NewRequest(http.MethodPost, "/generatedPlaylist/"+id, strings.NewReader(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		r = r.WithContext(context.WithValue(request.WithUser(r.Context(), alice), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		handler(w, r)
		var res map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		return w.Code, res
	}

	Describe("preview", func() {
		It("returns the preview of real Tracks for the Prompt", func() {
			code, res := call(previewGeneratedPlaylist(gen), `{"prompt":"rainy Sunday 90s indie","length":30}`)
			Expect(code).To(Equal(http.StatusOK))
			Expect(gen.gotPrompt).To(Equal("rainy Sunday 90s indie"))
			Expect(gen.gotLength).To(Equal(30))
			Expect(res["name"]).To(Equal("Rainy Sunday"))
			Expect(res["prompt"]).To(Equal("rainy Sunday 90s indie"))
			Expect(res["tracks"]).To(HaveLen(1))
			Expect(res["tracks"].([]any)[0]).To(HaveKeyWithValue("id", "mf-1"))
		})

		It("is not found when no LLM is configured", func() {
			gen.enabled = false
			code, _ := call(previewGeneratedPlaylist(gen), `{"prompt":"indie"}`)
			Expect(code).To(Equal(http.StatusNotFound))
			Expect(gen.gotPrompt).To(BeEmpty())
		})

		It("says the AI is unavailable when the LLM can't be reached", func() {
			gen.err = fmt.Errorf("filters: %w: dial tcp 100.64.0.7:11434: i/o timeout", llm.ErrUnavailable)
			code, res := call(previewGeneratedPlaylist(gen), `{"prompt":"indie"}`)
			Expect(code).To(Equal(http.StatusServiceUnavailable))
			Expect(res).To(HaveKeyWithValue("error", "aiUnavailable"))
			// The internal address is logged, not shown.
			Expect(fmt.Sprint(res)).ToNot(ContainSubstring("100.64.0.7"))
		})

		It("reports a reply the server couldn't use", func() {
			gen.err = fmt.Errorf("%w: unexpected end of JSON input", llm.ErrInvalidReply)
			code, res := call(previewGeneratedPlaylist(gen), `{"prompt":"indie"}`)
			Expect(code).To(Equal(http.StatusBadGateway))
			Expect(res).To(HaveKeyWithValue("error", "aiInvalidReply"))
		})

		It("rejects a bad Prompt or length", func() {
			gen.err = generatedplaylist.ErrInvalidLength
			code, res := call(previewGeneratedPlaylist(gen), `{"prompt":"indie","length":500}`)
			Expect(code).To(Equal(http.StatusBadRequest))
			Expect(res).To(HaveKeyWithValue("error", "invalidLength"))

			gen.err = generatedplaylist.ErrInvalidPrompt
			code, res = call(previewGeneratedPlaylist(gen), `{"prompt":""}`)
			Expect(code).To(Equal(http.StatusBadRequest))
			Expect(res).To(HaveKeyWithValue("error", "invalidPrompt"))

			code, _ = call(previewGeneratedPlaylist(gen), `not json`)
			Expect(code).To(Equal(http.StatusBadRequest))
		})

		It("reports an empty Library", func() {
			gen.err = generatedplaylist.ErrNoTracks
			code, res := call(previewGeneratedPlaylist(gen), `{"prompt":"indie"}`)
			Expect(code).To(Equal(http.StatusUnprocessableEntity))
			Expect(res).To(HaveKeyWithValue("error", "noTracks"))
		})
	})

	Describe("save", func() {
		It("saves the preview as the Listener's Playlist", func() {
			code, res := call(saveGeneratedPlaylist(gen), `{"name":"Rainy Sunday","prompt":"rainy Sunday 90s indie","trackIds":["mf-2","mf-1"]}`)
			Expect(code).To(Equal(http.StatusOK))
			Expect(res).To(HaveKeyWithValue("id", "pls-1"))
			Expect(gen.gotLength).To(Equal(0))
			Expect(gen.gotName).To(Equal("Rainy Sunday"))
			Expect(gen.gotPrompt).To(Equal("rainy Sunday 90s indie"))
			Expect(gen.gotTrackIDs).To(Equal([]string{"mf-2", "mf-1"}))
			Expect(gen.gotUser).To(Equal("u1"))
		})

		It("is not found when no LLM is configured", func() {
			gen.enabled = false
			code, _ := call(saveGeneratedPlaylist(gen), `{"prompt":"indie","trackIds":["mf-1"]}`)
			Expect(code).To(Equal(http.StatusNotFound))
		})

		It("rejects a Playlist with no real Tracks", func() {
			gen.err = generatedplaylist.ErrNoTracks
			code, res := call(saveGeneratedPlaylist(gen), `{"prompt":"indie","trackIds":["made-up"]}`)
			Expect(code).To(Equal(http.StatusUnprocessableEntity))
			Expect(res).To(HaveKeyWithValue("error", "noTracks"))
		})
	})

	Describe("regenerate", func() {
		It("returns a fresh preview of the Generated Playlist", func() {
			code, res := callPlaylist(regenerateGeneratedPlaylist(gen), "pls-1", "")
			Expect(code).To(Equal(http.StatusOK))
			Expect(gen.gotID).To(Equal("pls-1"))
			Expect(res).To(HaveKeyWithValue("prompt", "rainy Sunday 90s indie"))
			Expect(res).To(HaveKeyWithValue("length", BeNumerically("==", 12)))
			Expect(res["tracks"]).To(HaveLen(1))
		})

		It("is not found when no LLM is configured", func() {
			gen.enabled = false
			code, _ := callPlaylist(regenerateGeneratedPlaylist(gen), "pls-1", "")
			Expect(code).To(Equal(http.StatusNotFound))
			Expect(gen.gotID).To(BeEmpty())
		})

		It("refuses an ordinary Playlist", func() {
			gen.err = generatedplaylist.ErrNotGenerated
			code, res := callPlaylist(regenerateGeneratedPlaylist(gen), "pls-1", "")
			Expect(code).To(Equal(http.StatusConflict))
			Expect(res).To(HaveKeyWithValue("error", "notGenerated"))
		})

		It("is not found for a Playlist the Listener can't see", func() {
			gen.err = model.ErrNotFound
			code, res := callPlaylist(regenerateGeneratedPlaylist(gen), "pls-1", "")
			Expect(code).To(Equal(http.StatusNotFound))
			Expect(res).To(HaveKeyWithValue("error", "notFound"))
		})
	})

	Describe("replace", func() {
		It("replaces the Generated Playlist's Tracks with the preview's", func() {
			code, _ := callPlaylist(replaceGeneratedPlaylist(gen), "pls-1", `{"trackIds":["mf-3","mf-1"]}`)
			Expect(code).To(Equal(http.StatusOK))
			Expect(gen.gotID).To(Equal("pls-1"))
			Expect(gen.gotTrackIDs).To(Equal([]string{"mf-3", "mf-1"}))
			Expect(gen.gotUser).To(Equal("u1"))
		})

		It("is not found when no LLM is configured", func() {
			gen.enabled = false
			code, _ := callPlaylist(replaceGeneratedPlaylist(gen), "pls-1", `{"trackIds":["mf-1"]}`)
			Expect(code).To(Equal(http.StatusNotFound))
			Expect(gen.gotID).To(BeEmpty())
		})

		It("refuses another Listener's Playlist", func() {
			gen.err = model.ErrNotAuthorized
			code, res := callPlaylist(replaceGeneratedPlaylist(gen), "pls-1", `{"trackIds":["mf-1"]}`)
			Expect(code).To(Equal(http.StatusForbidden))
			Expect(res).To(HaveKeyWithValue("error", "notAuthorized"))
		})

		It("rejects a body that isn't JSON", func() {
			code, _ := callPlaylist(replaceGeneratedPlaylist(gen), "pls-1", `not json`)
			Expect(code).To(Equal(http.StatusBadRequest))
			Expect(gen.gotID).To(BeEmpty())
		})
	})

	Describe("routes", func() {
		BeforeEach(func() {
			DeferCleanup(configtest.SetupConfig())
		})

		It("needs a logged-in Listener", func() {
			conf.Server.LLM.BaseURL = "http://127.0.0.1:1"
			conf.Server.LLM.Model = "m"
			conf.Server.EnableSharing = false
			ds := &tests.MockDataStore{}
			router := New(ds, nil, nil, nil, tests.NewMockLibraryService(), core.NewUser(ds, noopPluginUnloader{}), nil, nil, nil, nil, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/generatedPlaylist/preview", strings.NewReader(`{"prompt":"indie"}`)))
			Expect(w.Code).To(Equal(http.StatusUnauthorized))
		})
	})
})
