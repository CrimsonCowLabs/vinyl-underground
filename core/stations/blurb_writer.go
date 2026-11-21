package stations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/navidrome/navidrome/core/llm"
	"github.com/navidrome/navidrome/model"
)

// maxBlurbLength is the longest Blurb shown, in characters: a few lines on screen. The LLM is
// asked for much less; a longer reply is one that rambled.
const maxBlurbLength = 400

var errBadBlurb = errors.New("the AI's Blurb doesn't fit")

const blurbSystem = `You are %s, the DJ of a personal music Station. Your persona: %s

Between two Tracks you show a Blurb: a short on-screen text, in your own voice and tone, that looks back on the Track that just played and introduces the next one. It is read on screen, never spoken.
- Write 1 to 3 short sentences, under 45 words.
- Mention the next Track by its title or its artist, and the one that just played too.
- Only use the facts given about the Tracks. Never invent trivia, dates, or other songs.
- No hashtags, emoji, or quotes around the whole text.`

var blurbSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"blurb": map[string]any{"type": "string"},
	},
	"required":             []string{"blurb"},
	"additionalProperties": false,
}

// NewBlurbWriter returns the BlurbWriter that has the LLM write Blurbs.
func NewBlurbWriter(client llm.Client) BlurbWriter {
	return &llmBlurbWriter{llm: client}
}

type llmBlurbWriter struct {
	llm llm.Client
}

func (w *llmBlurbWriter) Write(ctx context.Context, dj DJ, theme string, prev, next model.MediaFile) (string, error) {
	user := fmt.Sprintf("Just played: %s\nUp next: %s", trackLine(prev), trackLine(next))
	if theme != "" {
		user += "\nTheme: " + theme
	}
	var reply struct {
		Blurb string `json:"blurb"`
	}
	req := llm.Request{System: fmt.Sprintf(blurbSystem, dj.Name, dj.Description), User: user, SchemaName: "blurb", Schema: blurbSchema}
	if err := w.llm.Complete(ctx, req, &reply); err != nil {
		return "", err
	}
	text := strings.Join(strings.Fields(reply.Blurb), " ")
	switch {
	case text == "":
		return "", fmt.Errorf("%w: it is empty", errBadBlurb)
	case utf8.RuneCountInString(text) > maxBlurbLength:
		return "", fmt.Errorf("%w: it is too long", errBadBlurb)
	case !mentions(text, prev):
		return "", fmt.Errorf("%w: it doesn't mention the Track that just played", errBadBlurb)
	case !mentions(text, next):
		return "", fmt.Errorf("%w: it doesn't mention the next Track", errBadBlurb)
	}
	return text, nil
}

// trackLine describes a Track for the LLM in one line, with what the Library knows of it.
func trackLine(mf model.MediaFile) string {
	line := fmt.Sprintf("%q by %s", mf.Title, mf.Artist)
	if mf.Album != "" {
		line += ", from the album " + mf.Album
	}
	if mf.Year > 0 {
		line += fmt.Sprintf(" (%d)", mf.Year)
	}
	if mf.Genre != "" {
		line += ", " + mf.Genre
	}
	return line
}

// mentions reports whether the text names the Track, by its artist or its title (without a
// note such as "(Remastered 2020)" or "- Live"), in any case and as whole words, so the artist
// Air isn't found in "fair".
func mentions(text string, mf model.MediaFile) bool {
	text = strings.ToLower(text)
	title := mf.Title
	for _, sep := range []string{" (", " [", " - "} {
		if i := strings.Index(title, sep); i > 0 {
			title = title[:i]
		}
	}
	for _, name := range []string{mf.Artist, title} {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" && containsWords(text, name) {
			return true
		}
	}
	return false
}

// containsWords reports whether words is in text with no letter or digit right before or after.
func containsWords(text, words string) bool {
	for from := 0; ; {
		i := strings.Index(text[from:], words)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(words)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if !isWordRune(before) && !isWordRune(after) {
			return true
		}
		from = start + 1
	}
}

func isWordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}
