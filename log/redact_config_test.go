package log

import (
	"github.com/kr/pretty"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Redact configuration", func() {
	// The configuration is logged at startup, formatted like this
	It("redacts the LLM API key", func() {
		type llmOptions struct {
			BaseURL string
			APIKey  string
		}
		conf := struct{ LLM llmOptions }{llmOptions{BaseURL: "http://llm:11434", APIKey: "sk-proj_a1.b2-c3"}}
		got := Redact(pretty.Sprintf("Configuration: %# v", conf))
		Expect(got).ToNot(ContainSubstring("a1.b2-c3"))
		Expect(got).To(ContainSubstring("http://llm:11434"))
	})
})
