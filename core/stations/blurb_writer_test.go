package stations_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/navidrome/navidrome/core/llm"
	"github.com/navidrome/navidrome/core/stations"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeLLM answers every request with reply, as the LLM's JSON, and remembers the requests.
type fakeLLM struct {
	requests []llm.Request
	reply    string
	err      error
}

func (f *fakeLLM) Complete(_ context.Context, req llm.Request, out any) error {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return f.err
	}
	return json.Unmarshal([]byte(f.reply), out)
}

var _ = Describe("BlurbWriter", func() {
	var client *fakeLLM
	var writer stations.BlurbWriter
	owl, _ := stations.FindDJ("night-owl")
	prev := model.MediaFile{ID: "1", Title: "Blue in Green", Artist: "Miles Davis", Album: "Kind of Blue", Year: 1959}
	next := model.MediaFile{ID: "2", Title: "Naima (Remastered 2020)", Artist: "John Coltrane", Album: "Giant Steps", Year: 1960}

	BeforeEach(func() {
		client = &fakeLLM{}
		writer = stations.NewBlurbWriter(client)
	})

	reply := func(text string) string {
		b, _ := json.Marshal(map[string]string{"blurb": text})
		return string(b)
	}

	It("asks in the DJ's persona about the Track that played and the next, with the Theme", func() {
		client.reply = reply("That was Miles Davis. Now, Naima by John Coltrane.")

		text, err := writer.Write(context.Background(), owl, "rainy night", prev, next)

		Expect(err).ToNot(HaveOccurred())
		Expect(text).To(Equal("That was Miles Davis. Now, Naima by John Coltrane."))
		Expect(client.requests).To(HaveLen(1))
		req := client.requests[0]
		Expect(req.System).To(ContainSubstring(owl.Name))
		Expect(req.System).To(ContainSubstring(owl.Description))
		Expect(req.Schema).ToNot(BeEmpty())
		for _, want := range []string{"Blue in Green", "Miles Davis", "Kind of Blue", "1959", "Naima", "John Coltrane", "Theme: rainy night"} {
			Expect(req.User).To(ContainSubstring(want))
		}
	})

	It("leaves out the Theme when there is none", func() {
		client.reply = reply("After Miles Davis, here's John Coltrane.")
		_, err := writer.Write(context.Background(), owl, "", prev, next)
		Expect(err).ToNot(HaveOccurred())
		Expect(client.requests[0].User).ToNot(ContainSubstring("Theme"))
	})

	It("tidies the text: one line, trimmed", func() {
		client.reply = reply("  Blue in Green, and up next:\n\n  Naima.  ")
		text, err := writer.Write(context.Background(), owl, "", prev, next)
		Expect(err).ToNot(HaveOccurred())
		Expect(text).To(Equal("Blue in Green, and up next: Naima."))
	})

	It("refuses a Blurb that doesn't mention both Tracks, by title or artist", func() {
		for _, text := range []string{
			"What a night for some music, friends.",
			"Coming up, Naima by John Coltrane.",  // nothing about what just played
			"That was Miles Davis. Stay with me.", // nothing about what's next
		} {
			client.reply = reply(text)
			_, err := writer.Write(context.Background(), owl, "", prev, next)
			Expect(err).To(HaveOccurred(), text)
		}

		client.reply = reply("After miles davis, coming up, NAIMA.") // the title without its remaster note, any case
		_, err := writer.Write(context.Background(), owl, "", prev, next)
		Expect(err).ToNot(HaveOccurred())
	})

	It("only counts a short name as mentioned when it is a whole word", func() {
		air := model.MediaFile{ID: "3", Title: "La Femme d'Argent", Artist: "Air"}
		client.reply = reply("Blue in Green was fair and fine; and now, something airy.")
		_, err := writer.Write(context.Background(), owl, "", prev, air)
		Expect(err).To(HaveOccurred())

		client.reply = reply("Blue in Green, then Air.")
		_, err = writer.Write(context.Background(), owl, "", prev, air)
		Expect(err).ToNot(HaveOccurred())
	})

	It("refuses an empty or rambling Blurb", func() {
		client.reply = reply("   ")
		_, err := writer.Write(context.Background(), owl, "", prev, next)
		Expect(err).To(HaveOccurred())

		client.reply = reply("Miles Davis, John Coltrane " + strings.Repeat("and on ", 200))
		_, err = writer.Write(context.Background(), owl, "", prev, next)
		Expect(err).To(HaveOccurred())
	})

	It("fails when the LLM does", func() {
		client.err = llm.ErrUnavailable
		_, err := writer.Write(context.Background(), owl, "", prev, next)
		Expect(errors.Is(err, llm.ErrUnavailable)).To(BeTrue())
	})
})
