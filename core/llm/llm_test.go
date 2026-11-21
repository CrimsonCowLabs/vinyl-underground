package llm_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/navidrome/navidrome/core/llm"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type filters struct {
	Genres []string `json:"genres"`
}

var filtersSchema = map[string]any{
	"type":       "object",
	"properties": map[string]any{"genres": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
	"required":   []string{"genres"},
}

// chatReply is an OpenAI-compatible chat completion whose message content is content.
func chatReply(content string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}},
	})
	return string(b)
}

var _ = Describe("Client", func() {
	var req llm.Request

	BeforeEach(func() {
		req = llm.Request{
			System:     "Turn the Prompt into filters.",
			User:       "rainy Sunday 90s indie",
			SchemaName: "filters",
			Schema:     filtersSchema,
		}
	})

	Describe("Configured", func() {
		It("is configured only with both a URL and a model name", func() {
			Expect(llm.Configured(llm.Options{BaseURL: "http://llm-host:11434", Model: "qwen3.5:4b-q4_K_M"})).To(BeTrue())
			Expect(llm.Configured(llm.Options{BaseURL: "http://llm-host:11434"})).To(BeFalse())
			Expect(llm.Configured(llm.Options{Model: "qwen3.5:4b-q4_K_M"})).To(BeFalse())
			Expect(llm.Configured(llm.Options{BaseURL: "  ", Model: " "})).To(BeFalse())
		})
	})

	It("asks for schema-constrained output with thinking off, and decodes the reply", func() {
		var got map[string]any
		var path, auth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			auth = r.Header.Get("Authorization")
			body, _ := io.ReadAll(r.Body)
			Expect(json.Unmarshal(body, &got)).To(Succeed())
			_, _ = io.WriteString(w, chatReply(`{"genres":["Indie Rock"]}`))
		}))
		DeferCleanup(srv.Close)

		client := llm.New(llm.Options{BaseURL: srv.URL + "/v1", Model: "qwen3.5:4b-q4_K_M", APIKey: "secret"})
		var out filters
		Expect(client.Complete(GinkgoT().Context(), req, &out)).To(Succeed())

		Expect(out.Genres).To(Equal([]string{"Indie Rock"}))
		Expect(path).To(Equal("/v1/chat/completions"))
		Expect(auth).To(Equal("Bearer secret"))
		Expect(got["model"]).To(Equal("qwen3.5:4b-q4_K_M"))
		Expect(got["stream"]).To(Equal(false))
		Expect(got["reasoning_effort"]).To(Equal("none"))
		Expect(got["messages"]).To(Equal([]any{
			map[string]any{"role": "system", "content": "Turn the Prompt into filters."},
			map[string]any{"role": "user", "content": "rainy Sunday 90s indie"},
		}))
		Expect(got["response_format"]).To(Equal(map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "filters",
				"strict": true,
				"schema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"genres": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
					"required":   []any{"genres"},
				},
			},
		}))
	})

	It("adds /v1 to a URL with no path, and sends no Authorization without an API key", func() {
		var path, auth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path, auth = r.URL.Path, r.Header.Get("Authorization")
			_, _ = io.WriteString(w, chatReply(`{"genres":[]}`))
		}))
		DeferCleanup(srv.Close)

		var out filters
		Expect(llm.New(llm.Options{BaseURL: srv.URL, Model: "m"}).Complete(GinkgoT().Context(), req, &out)).To(Succeed())
		Expect(path).To(Equal("/v1/chat/completions"))
		Expect(auth).To(BeEmpty())
	})

	It("reports an endpoint nothing is listening on as unavailable", func() {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())
		addr := l.Addr().String()
		Expect(l.Close()).To(Succeed())

		var out filters
		err = llm.New(llm.Options{BaseURL: "http://" + addr, Model: "m"}).Complete(GinkgoT().Context(), req, &out)
		Expect(err).To(MatchError(llm.ErrUnavailable))
	})

	It("gives up on an endpoint that can't be reached within the connect timeout", func() {
		// 10.255.255.1 is unroutable, so the connection attempt hangs like one to a sleeping host.
		client := llm.New(llm.Options{BaseURL: "http://10.255.255.1:11434", Model: "m",
			ConnectTimeout: 200 * time.Millisecond, Timeout: time.Minute})
		start := time.Now()
		var out filters
		err := client.Complete(GinkgoT().Context(), req, &out)
		Expect(err).To(MatchError(llm.ErrUnavailable))
		Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
	})

	It("reports a reply slower than the timeout as unavailable", func() {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}))
		DeferCleanup(srv.Close)
		DeferCleanup(func() { close(release) })

		client := llm.New(llm.Options{BaseURL: srv.URL, Model: "m", Timeout: 100 * time.Millisecond})
		var out filters
		Expect(client.Complete(GinkgoT().Context(), req, &out)).To(MatchError(llm.ErrUnavailable))
	})

	It("reports an error status from the endpoint as unavailable", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
		}))
		DeferCleanup(srv.Close)

		var out filters
		err := llm.New(llm.Options{BaseURL: srv.URL, Model: "m"}).Complete(GinkgoT().Context(), req, &out)
		Expect(err).To(MatchError(llm.ErrUnavailable))
		Expect(err.Error()).To(ContainSubstring("model not found"))
	})

	It("rejects a reply that isn't JSON", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, chatReply("Sure! Here are some genres: indie"))
		}))
		DeferCleanup(srv.Close)

		var out filters
		err := llm.New(llm.Options{BaseURL: srv.URL, Model: "m"}).Complete(GinkgoT().Context(), req, &out)
		Expect(err).To(MatchError(llm.ErrInvalidReply))
	})

	It("rejects a reply with no choices", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"choices":[]}`)
		}))
		DeferCleanup(srv.Close)

		var out filters
		err := llm.New(llm.Options{BaseURL: srv.URL, Model: "m"}).Complete(GinkgoT().Context(), req, &out)
		Expect(err).To(MatchError(llm.ErrInvalidReply))
	})
})
