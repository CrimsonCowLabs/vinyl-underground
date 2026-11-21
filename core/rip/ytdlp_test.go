package rip

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeYtDlp writes a shell script standing in for yt-dlp. It records its arguments (one per line)
// and its TMPDIR next to itself, then runs body.
func fakeYtDlp(body string) (path string, args func() []string, tmpdir func() string) {
	dir := GinkgoT().TempDir()
	path = filepath.Join(dir, "yt-dlp")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$0.args\"\nprintf '%s' \"$TMPDIR\" > \"$0.tmpdir\"\n" + body + "\n"
	Expect(os.WriteFile(path, []byte(script), 0o755)).To(Succeed())
	args = func() []string {
		b, err := os.ReadFile(path + ".args")
		Expect(err).ToNot(HaveOccurred())
		return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	}
	tmpdir = func() string {
		b, err := os.ReadFile(path + ".tmpdir")
		Expect(err).ToNot(HaveOccurred())
		return string(b)
	}
	return path, args, tmpdir
}

type fixedBinary string

func (b fixedBinary) Path(context.Context) (string, error) { return string(b), nil }

const hostileURL = "https://example.com/watch?v=1;rm -rf /&x=$(id)"

var _ = Describe("YtDlp", func() {
	var tmp string

	BeforeEach(func() {
		tmp = GinkgoT().TempDir()
	})

	Describe("Probe", func() {
		It("reads the page's metadata without downloading and without user config", func(ctx SpecContext) {
			bin, args, _ := fakeYtDlp(`cat <<'EOF'
{"_type":"video","id":"abc","title":"Song Title","uploader":"Some Channel","extractor_key":"Youtube","webpage_url":"https://www.youtube.com/watch?v=abc"}
EOF`)
			y := &YtDlp{Binary: fixedBinary(bin), TempDir: tmp}

			info, err := y.Probe(ctx, hostileURL)
			Expect(err).ToNot(HaveOccurred())
			Expect(info).To(Equal(&Info{
				ID: "abc", Title: "Song Title", Artist: "Some Channel",
				Source: "YouTube", SourceURL: "https://www.youtube.com/watch?v=abc", Count: 1,
			}))

			a := args()
			Expect(a).To(ContainElements("--ignore-config", "--no-playlist", "--dump-single-json"))
			Expect(a[len(a)-2:]).To(Equal([]string{"--", hostileURL}), "the URL comes last, after --, as one argument")
		})

		It("prefers the track's own artist and title when the site has them", func(ctx SpecContext) {
			bin, _, _ := fakeYtDlp(`echo '{"id":"x","title":"Artist - Song (Official)","track":"Song","artists":["Artist","Guest"],"uploader":"Artist - Topic","extractor_key":"Youtube","webpage_url":"https://www.youtube.com/watch?v=x"}'`)
			info, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Probe(ctx, "https://music.youtube.com/watch?v=x")
			Expect(err).ToNot(HaveOccurred())
			Expect(info.Title).To(Equal("Song"))
			Expect(info.Artist).To(Equal("Artist, Guest"))
		})

		It("tells a playlist apart from a single video", func(ctx SpecContext) {
			bin, _, _ := fakeYtDlp(`echo '{"_type":"playlist","id":"PL1","title":"A Playlist","extractor_key":"YoutubeTab","webpage_url":"https://www.youtube.com/playlist?list=PL1"}'`)
			info, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Probe(ctx, "https://www.youtube.com/playlist?list=PL1")
			Expect(err).ToNot(HaveOccurred())
			Expect(info.IsPlaylist).To(BeTrue())
		})

		It("lists a playlist's items, without looking at each one, up to one past the limit", func(ctx SpecContext) {
			bin, args, _ := fakeYtDlp(`cat <<'EOF'
{"_type":"playlist","id":"OLAK5uy_x","title":"Album - Great Album","playlist_count":3,"extractor_key":"YoutubeTab","webpage_url":"https://music.youtube.com/playlist?list=OLAK5uy_x",
 "entries":[
  {"_type":"url","ie_key":"Youtube","id":"a1","url":"https://music.youtube.com/watch?v=a1","title":"First","uploader":"Band - Topic"},
  {"_type":"url","ie_key":"Youtube","id":"a2","url":"https://music.youtube.com/watch?v=a2","title":"Second","channel":"Band"},
  {"_type":"url","ie_key":"Youtube","id":"a3","url":"https://music.youtube.com/watch?v=a3","title":"[Private video]"}
 ]}
EOF`)
			info, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Probe(ctx, "https://music.youtube.com/playlist?list=OLAK5uy_x")
			Expect(err).ToNot(HaveOccurred())
			Expect(info.IsPlaylist).To(BeTrue())
			Expect(info.Title).To(Equal("Great Album"), "YouTube Music's \"Album - \" prefix is dropped")
			Expect(info.Source).To(Equal("YouTube"))
			Expect(info.Count).To(Equal(3))
			Expect(info.Entries).To(Equal([]PlaylistEntry{
				{URL: "https://music.youtube.com/watch?v=a1", Title: "First", Artist: "Band"},
				{URL: "https://music.youtube.com/watch?v=a2", Title: "Second", Artist: "Band"},
				{URL: "https://music.youtube.com/watch?v=a3", Title: "[Private video]"},
			}))
			Expect(args()).To(ContainElements("--flat-playlist", "--playlist-items", "1:301"))
		})

		It("counts a playlist by yt-dlp's total when it lists only the first items", func(ctx SpecContext) {
			bin, _, _ := fakeYtDlp(`echo '{"_type":"playlist","id":"PL1","title":"Huge","playlist_count":1200,"entries":[{"id":"a","url":"https://www.youtube.com/watch?v=a","title":"A"}]}'`)
			info, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Probe(ctx, "https://www.youtube.com/playlist?list=PL1")
			Expect(err).ToNot(HaveOccurred())
			Expect(info.Count).To(Equal(1200))
		})

		It("counts a single video as one item", func(ctx SpecContext) {
			bin, _, _ := fakeYtDlp(`echo '{"_type":"video","id":"abc","title":"Song","webpage_url":"https://www.youtube.com/watch?v=abc"}'`)
			info, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Probe(ctx, "https://www.youtube.com/watch?v=abc")
			Expect(err).ToNot(HaveOccurred())
			Expect(info.IsPlaylist).To(BeFalse())
			Expect(info.Count).To(Equal(1))
			Expect(info.Entries).To(BeEmpty())
		})

		It("returns yt-dlp's error message when it fails", func(ctx SpecContext) {
			bin, _, _ := fakeYtDlp(`echo "WARNING: something minor" >&2
echo "ERROR: [youtube] abc: Video unavailable" >&2
exit 1`)
			_, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Probe(ctx, "https://www.youtube.com/watch?v=abc")
			Expect(err).To(MatchError("ERROR: [youtube] abc: Video unavailable"))
		})
	})

	Describe("Download", func() {
		// Writes a fake File named after the -o template's folder (-P) and prints its path, like
		// yt-dlp's --print after_move:filepath.
		const writesFile = `while [ $# -gt 0 ]; do [ "$1" = "-P" ] && dir="$2"; shift; done
printf 'audio' > "$dir/abc.opus"
echo "[download] 100%"
echo "$dir/abc.opus"`

		It("keeps the best original Opus or AAC stream, remuxed, never re-encoded", func(ctx SpecContext) {
			bin, args, _ := fakeYtDlp(writesFile)
			y := &YtDlp{Binary: fixedBinary(bin), TempDir: tmp, FFmpegPath: "/usr/bin/ffmpeg"}
			out := GinkgoT().TempDir()

			path, err := y.Download(ctx, hostileURL, out)
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal(filepath.Join(out, "abc.opus")))

			a := args()
			Expect(a).To(ContainElements("--ignore-config", "--no-playlist", "--extract-audio"))
			Expect(a).To(ContainElements("-f", "bestaudio[acodec^=opus]/bestaudio[acodec^=mp4a]/best[acodec^=opus]/best[acodec^=mp4a]"))
			Expect(a).To(ContainElements("--audio-format", "best"), "best keeps the original codec (no re-encode)")
			Expect(a).To(ContainElements("--ffmpeg-location", "/usr/bin/ffmpeg"))
			Expect(a).To(ContainElements("-P", out))
			Expect(a).ToNot(ContainElement("--audio-quality"))
			Expect(a[len(a)-2:]).To(Equal([]string{"--", hostileURL}))
		})

		It("runs yt-dlp with its own temp folder, as /tmp may not allow running programs", func(ctx SpecContext) {
			bin, _, tmpdir := fakeYtDlp(writesFile)
			_, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Download(ctx, "https://example.com/v", GinkgoT().TempDir())
			Expect(err).ToNot(HaveOccurred())
			Expect(tmpdir()).To(Equal(tmp))
		})

		It("points yt-dlp at deno for YouTube's JavaScript, with a writable deno cache", func(ctx SpecContext) {
			bin, args, _ := fakeYtDlp(`printf '%s' "$DENO_DIR" > "$0.denodir"
` + writesFile)
			denoDir := filepath.Join(tmp, "deno-cache")
			y := &YtDlp{Binary: fixedBinary(bin), TempDir: tmp, DenoPath: "/usr/bin/deno", DenoDir: denoDir}

			_, err := y.Download(ctx, "https://example.com/v", GinkgoT().TempDir())
			Expect(err).ToNot(HaveOccurred())
			Expect(args()).To(ContainElements("--js-runtimes", "deno:/usr/bin/deno"))
			Expect(os.ReadFile(bin + ".denodir")).To(Equal([]byte(denoDir)))

			_, err = y.Probe(ctx, "https://example.com/v")
			Expect(err).To(HaveOccurred()) // the fake prints no JSON; only the argv matters here
			Expect(args()).To(ContainElements("--js-runtimes", "deno:/usr/bin/deno"))
		})

		It("leaves the JavaScript runtime to yt-dlp when there is no deno", func(ctx SpecContext) {
			bin, args, _ := fakeYtDlp(writesFile)
			_, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Download(ctx, "https://example.com/v", GinkgoT().TempDir())
			Expect(err).ToNot(HaveOccurred())
			Expect(args()).ToNot(ContainElement("--js-runtimes"))
		})

		It("clears what a killed yt-dlp left in its temp folder", func(ctx SpecContext) {
			stale := filepath.Join(tmp, "_MEI12345")
			Expect(os.MkdirAll(stale, 0o755)).To(Succeed())
			bin, _, _ := fakeYtDlp(writesFile)
			_, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Download(ctx, "https://example.com/v", GinkgoT().TempDir())
			Expect(err).ToNot(HaveOccurred())
			Expect(stale).ToNot(BeAnExistingFile())
		})

		It("refuses a result that isn't .opus or .m4a", func(ctx SpecContext) {
			bin, _, _ := fakeYtDlp(`while [ $# -gt 0 ]; do [ "$1" = "-P" ] && dir="$2"; shift; done
printf 'audio' > "$dir/abc.mp3"; echo "$dir/abc.mp3"`)
			_, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Download(ctx, "https://example.com/v", GinkgoT().TempDir())
			Expect(err).To(MatchError(ContainSubstring(".mp3")))
		})

		It("explains when the page has no Opus or AAC stream", func(ctx SpecContext) {
			bin, _, _ := fakeYtDlp(`echo "ERROR: [generic] v: Requested format is not available. Use --list-formats for a list of available formats" >&2; exit 1`)
			_, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Download(ctx, "https://example.com/v", GinkgoT().TempDir())
			Expect(err).To(MatchError(ContainSubstring("no Opus or AAC audio")))
			Expect(err).To(MatchError(ContainSubstring("Requested format is not available")))
		})

		It("runs one yt-dlp at a time, so a probe never clears the temp folder of a running download", func(ctx SpecContext) {
			bin, _, _ := fakeYtDlp(`touch "$TMPDIR/run-$$"; sleep 0.5
[ -e "$TMPDIR/run-$$" ] || { echo "ERROR: temp folder cleared under me" >&2; exit 1; }
echo '{"id":"x","title":"T"}'`)
			y := &YtDlp{Binary: fixedBinary(bin), TempDir: tmp}
			errs := make(chan error, 2)
			probe := func() { _, err := y.Probe(ctx, "https://example.com/v"); errs <- err }
			go probe()
			Eventually(func() ([]os.DirEntry, error) { return os.ReadDir(tmp) }).ShouldNot(BeEmpty())
			go probe()
			Expect(<-errs).ToNot(HaveOccurred())
			Expect(<-errs).ToNot(HaveOccurred())
		})

		It("stops yt-dlp when the context ends", func(ctx SpecContext) {
			bin, _, _ := fakeYtDlp(`sleep 30`)
			c, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := (&YtDlp{Binary: fixedBinary(bin), TempDir: tmp}).Download(c, "https://example.com/v", GinkgoT().TempDir())
			Expect(err).To(HaveOccurred())
			Expect(time.Since(start)).To(BeNumerically("<", 10*time.Second))
		})
	})

	DescribeTable("SourceName gives the site's display name",
		func(extractorKey, want string) {
			Expect(SourceName(extractorKey)).To(Equal(want))
		},
		Entry("YouTube", "Youtube", "YouTube"),
		Entry("YouTube tab", "YoutubeTab", "YouTube"),
		Entry("SoundCloud", "Soundcloud", "SoundCloud"),
		Entry("Bandcamp", "Bandcamp", "Bandcamp"),
		Entry("unknown site, as yt-dlp names it", "Vimeo", "Vimeo"),
	)
})
