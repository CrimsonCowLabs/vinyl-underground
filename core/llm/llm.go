// Package llm talks to the local LLM behind Generated Playlists (ADR-0004): any
// OpenAI-compatible chat endpoint (Ollama, llama.cpp, LM Studio), set by a URL and model name.
//
// Every call constrains the reply with a JSON schema, because prompting a ~4B model for JSON
// alone is not reliable. It uses the OpenAI-compatible route, POST <base>/chat/completions with
// response_format {type: json_schema}, which every such server understands; Ollama turns it into
// its native `format` option. Thinking is turned off with reasoning_effort "none", which Ollama
// honours on this route (its native `think: false` is ignored here). Checked against Ollama
// 0.40.16 serving qwen3.5:4b-q4_K_M: the schema is enforced (asked for a poem, it still returned
// the schema's JSON), and with thinking left on a filters call took 92 s instead of 8 s.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
)

var (
	// ErrUnavailable means the LLM could not be reached or did not answer in time, e.g. its
	// host is asleep. Listeners see it as "AI unavailable".
	ErrUnavailable = errors.New("AI unavailable")
	// ErrInvalidReply means the LLM answered, but not with JSON matching the request.
	ErrInvalidReply = errors.New("invalid reply from the AI")
)

// Options configures the endpoint. Zero timeouts use the defaults in consts.
type Options struct {
	BaseURL        string
	Model          string
	APIKey         string
	Timeout        time.Duration
	ConnectTimeout time.Duration
}

// Request is one schema-constrained chat call.
type Request struct {
	System     string
	User       string
	SchemaName string
	Schema     map[string]any
}

// Client sends a Request and decodes the JSON reply into out.
type Client interface {
	Complete(ctx context.Context, req Request, out any) error
}

// Configured reports whether opts names an endpoint and a model. Without both, AI features
// are hidden.
func Configured(opts Options) bool {
	return strings.TrimSpace(opts.BaseURL) != "" && strings.TrimSpace(opts.Model) != ""
}

// FromConfig returns the Options set by the LLM.* config (ND_LLM_*).
func FromConfig() Options {
	c := conf.Server.LLM
	return Options{BaseURL: c.BaseURL, Model: c.Model, APIKey: c.APIKey, Timeout: c.Timeout, ConnectTimeout: c.ConnectTimeout}
}

// New returns a Client for an OpenAI-compatible endpoint.
func New(opts Options) Client {
	if opts.Timeout <= 0 {
		opts.Timeout = consts.DefaultLLMTimeout
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = consts.DefaultLLMConnectTimeout
	}
	dialer := &net.Dialer{Timeout: opts.ConnectTimeout}
	return &client{
		opts:     opts,
		endpoint: chatEndpoint(opts.BaseURL),
		http: &http.Client{
			Timeout: opts.Timeout,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				DialContext:         dialer.DialContext,
				TLSHandshakeTimeout: opts.ConnectTimeout,
			},
		},
	}
}

// chatEndpoint appends /chat/completions to the OpenAI-style base URL, adding /v1 when the URL
// has no path (e.g. http://llm-host:11434).
func chatEndpoint(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if u, err := url.Parse(base); err == nil && u.Path == "" {
		base += "/v1"
	}
	return base + "/chat/completions"
}

type client struct {
	opts     Options
	endpoint string
	http     *http.Client
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type jsonSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type responseFormat struct {
	Type       string     `json:"type"`
	JSONSchema jsonSchema `json:"json_schema"`
}

type chatRequest struct {
	Model           string         `json:"model"`
	Messages        []message      `json:"messages"`
	ResponseFormat  responseFormat `json:"response_format"`
	ReasoningEffort string         `json:"reasoning_effort"`
	Temperature     float64        `json:"temperature"`
	Stream          bool           `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
}

func (c *client) Complete(ctx context.Context, req Request, out any) error {
	body, err := json.Marshal(chatRequest{
		Model: c.opts.Model,
		Messages: []message{
			{Role: "system", Content: req.System},
			{Role: "user", Content: req.User},
		},
		ResponseFormat: responseFormat{
			Type:       "json_schema",
			JSONSchema: jsonSchema{Name: req.SchemaName, Strict: true, Schema: req.Schema},
		},
		ReasoningEffort: "none",
		Temperature:     0.2,
	})
	if err != nil {
		return fmt.Errorf("encoding LLM request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.opts.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.opts.APIKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("%w: reading reply: %w", ErrUnavailable, err)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%w: %s: %s", ErrUnavailable, resp.Status, strings.TrimSpace(string(respBody)))
	}

	var chat chatResponse
	if err := json.Unmarshal(respBody, &chat); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidReply, err)
	}
	if len(chat.Choices) == 0 {
		return fmt.Errorf("%w: no choices", ErrInvalidReply)
	}
	if err := json.Unmarshal([]byte(chat.Choices[0].Message.Content), out); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidReply, err)
	}
	return nil
}
