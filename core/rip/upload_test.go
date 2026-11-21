package rip

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

// Uploads (#35): the admin's Files are streamed into the Inbox, checked, and become an upload
// job with a review card, never a Rip.
var _ = Describe("Upload", func() {
	var (
		ctx      context.Context
		ds       *tests.MockDataStore
		inbox    string
		proposer *fakeProposer
		matcher  *fakeMatcher
		ripper   *Ripper
	)

	BeforeEach(func() {
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: "admin", IsAdmin: true})
		inbox = GinkgoT().TempDir()
		libs := &tests.MockLibraryRepo{}
		libs.SetData(model.Libraries{{ID: 1, Name: "Music", Path: GinkgoT().TempDir()}})
		libs.PutFn = func(l *model.Library) error {
			if l.ID == 0 {
				l.ID = 2
			}
			libs.Data[l.ID] = *l
			return nil
		}
		ds = &tests.MockDataStore{MockedLibrary: libs}
		_, err := core.EnsureInboxLibrary(ctx, ds, inbox)
		Expect(err).ToNot(HaveOccurred())
		proposer = &fakeProposer{}
		matcher = &fakeMatcher{have: map[string]model.RipMatches{}}
		ripper = New(ds, newFakeFetcher(), &scanRecorder{}, Options{
			ScanRetryDelay: time.Millisecond, UploadScanDelay: time.Millisecond,
			Proposer: proposer, Matcher: matcher,
		})
	})

	fixture := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", name))
		Expect(err).ToNot(HaveOccurred())
		return data
	}
	upload := func(jobID, name string, data []byte) (*model.RipJob, *model.RipItem, error) {
		return ripper.Upload(ctx, jobID, name, bytes.NewReader(data))
	}
	inboxFiles := func() []string {
		var res []string
		_ = filepath.WalkDir(inbox, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				rel, _ := filepath.Rel(inbox, p)
				res = append(res, filepath.ToSlash(rel))
			}
			return nil
		})
		return res
	}
	refusal := func(err error) string {
		var refused *UploadRefusedError
		Expect(errors.As(err, &refused)).To(BeTrue(), "%v", err)
		return refused.Reason
	}

	DescribeTable("takes each accepted format, as it is",
		func(fixtureName, name string) {
			data := fixture(fixtureName)
			job, item, err := upload("", name, data)
			Expect(err).ToNot(HaveOccurred())

			Expect(job.Kind).To(Equal(model.RipKindUpload))
			Expect(job.Status).To(Equal(model.RipReady))
			Expect(job.URL).To(BeEmpty())
			Expect(job.Folder).To(HavePrefix("Upload "))
			Expect(item.Status).To(Equal(model.RipReady))
			Expect(item.Path).To(Equal(job.Folder + "/" + name))
			landed, err := os.ReadFile(filepath.Join(inbox, item.Path))
			Expect(err).ToNot(HaveOccurred())
			Expect(landed).To(Equal(data), "the File is kept byte for byte: no SOURCE tag is written")
		},
		Entry("mp3", "test.mp3", "song.mp3"),
		Entry("m4a", "test.m4a", "song.m4a"),
		Entry("opus", "test.opus", "song.opus"),
		Entry("ogg", "test.ogg", "song.ogg"),
		Entry("flac", "test.flac", "song.flac"),
		Entry("aiff", "test.aiff", "song.aiff"),
		Entry("wav", "test.wav", "song.wav"),
	)

	DescribeTable("refuses anything else, saying why, and keeps nothing of it",
		func(fixtureName, name, reason string) {
			_, _, err := upload("", name, fixture(fixtureName))
			Expect(refusal(err)).To(ContainSubstring(reason))
			Expect(inboxFiles()).To(BeEmpty())
			Expect(ds.RipJob(ctx).GetAll()).To(BeEmpty())
		},
		Entry("another audio format", "test.wma", "song.wma", "only mp3, m4a, opus, ogg, flac, aiff or wav"),
		Entry("no extension", "test.mp3", "song", "only mp3, m4a"),
		Entry("text named .mp3", "test.txt", "notes.mp3", "isn't MP3 audio"),
		Entry("a FLAC named .mp3", "test.flac", "song.mp3", "isn't MP3 audio"),
		Entry("a Vorbis Ogg named .opus", "test.ogg", "song.opus", "isn't OPUS audio"),
		Entry("an MP3 named .flac", "test.mp3", "song.flac", "isn't FLAC audio"),
	)

	It("refuses a File that only starts like audio, once TagLib can't read it", func() {
		_, _, err := upload("", "fake.flac", append([]byte("fLaC"), bytes.Repeat([]byte{0}, 4096)...))
		Expect(refusal(err)).To(ContainSubstring("doesn't read as FLAC audio"))
		Expect(inboxFiles()).To(BeEmpty())
	})

	It("refuses a File over the size limit while it streams, leaving no temp File", func() {
		data := fixture("test.mp3")
		ripper.opts.MaxUploadSize = int64(len(data) - 1)
		_, _, err := upload("", "big.mp3", data)
		Expect(refusal(err)).To(ContainSubstring("bigger than"))
		Expect(inboxFiles()).To(BeEmpty())

		ripper.opts.MaxUploadSize = int64(len(data))
		_, _, err = upload("", "fits.mp3", data)
		Expect(err).ToNot(HaveOccurred())
	})

	It("stops reading a too-big File at the limit", func() {
		ripper.opts.MaxUploadSize = 1 << 20
		body := &countingReader{r: io.MultiReader(bytes.NewReader(fixture("test.mp3")), endless{})}
		_, _, err := ripper.Upload(ctx, "", "huge.mp3", body)
		Expect(refusal(err)).To(ContainSubstring("bigger than 1 MB"))
		Expect(body.n).To(BeNumerically("<", 2<<20))
	})

	It("streams the File into a hidden temp File inside the Inbox, and only then moves it into place", func() {
		var during []string
		body := &onRead{r: bytes.NewReader(fixture("test.mp3")), after: 4096, fn: func() { during = inboxFiles() }}
		_, item, err := ripper.Upload(ctx, "", "song.mp3", body)
		Expect(err).ToNot(HaveOccurred())

		Expect(during).To(HaveLen(1))
		Expect(during[0]).To(HavePrefix(".uploads/"))
		Expect(inboxFiles()).To(ConsistOf(item.Path))
	})

	It("puts a batch's Files in one job, never over a File already there", func() {
		data := fixture("test.mp3")
		job, first, err := upload("", "song.mp3", data)
		Expect(err).ToNot(HaveOccurred())
		again, second, err := upload(job.ID, "song.mp3", data)
		Expect(err).ToNot(HaveOccurred())
		_, third, err := upload(job.ID, `C:\Music\other.MP3`, data)
		Expect(err).ToNot(HaveOccurred())

		Expect(again.ID).To(Equal(job.ID))
		Expect(first.Path).To(Equal(job.Folder + "/song.mp3"))
		Expect(second.Path).To(Equal(job.Folder + "/song (2).mp3"))
		Expect(third.Path).To(Equal(job.Folder + "/other.mp3"))
		Expect([]int{first.Position, second.Position, third.Position}).To(Equal([]int{1, 2, 3}))
		Expect(ds.RipJob(ctx).GetAll()).To(HaveLen(1))
	})

	It("gives each batch its own Inbox subfolder", func() {
		a, _, err := upload("", "song.mp3", fixture("test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		b, _, err := upload("", "song.mp3", fixture("test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(a.Folder).ToNot(Equal(b.Folder))
	})

	It("adds Files to an upload's card only", func() {
		rip := &model.RipJob{Kind: model.RipKindRip, Status: model.RipReady, URL: "https://www.youtube.com/watch?v=x"}
		Expect(ds.RipJob(ctx).Put(rip)).To(Succeed())
		_, _, err := upload(rip.ID, "song.mp3", fixture("test.mp3"))
		Expect(err).To(MatchError(ErrNotAnUpload))
		_, _, err = upload("gone", "song.mp3", fixture("test.mp3"))
		Expect(err).To(MatchError(model.ErrNotFound))
	})

	It("names the item after the File's tags, matches it, and proposes its tags from them", func() {
		have := model.RipMatches{{ID: "mf-9", Title: "Song", Artist: "Artist", Library: "Music"}}
		matcher.have["Artist|Song"] = have
		go ripper.runProposals(ctx)

		job, item, err := upload("", "upload.mp3", fixture("test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(item.Title).To(Equal("Song"))
		Expect(item.Artist).To(Equal("Artist"))

		items, err := ds.RipItem(ctx).ForJobs(job.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(items[0].AlreadyHave).To(Equal(have))
		Eventually(proposer.asked).Should(HaveLen(1))
		in := proposer.asked()[0]
		Expect(in.Artist).To(Equal("Artist"))
		Expect(in.Title).To(Equal("Song"))
		Expect(in.Album).To(Equal("Album"))
		Expect(in.Tagged).To(BeTrue())
		Expect(in.Duration).To(BeNumerically(">", 0))
	})

	It("never writes a SOURCE tag", func() {
		_, item, err := upload("", "song.opus", fixture("test.opus"))
		Expect(err).ToNot(HaveOccurred())
		tags, err := taglib.ReadTags(filepath.Join(inbox, item.Path))
		Expect(err).ToNot(HaveOccurred())
		Expect(tags).ToNot(HaveKey(TagSource))
		Expect(tags).ToNot(HaveKey(TagSourceURL))
	})

	It("is never picked up by the ripping worker", func() {
		_, _, err := upload("", "song.mp3", fixture("test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		_, err = ds.RipJob(ctx).NextQueued()
		Expect(err).To(MatchError(model.ErrNotFound))
	})

	It("refuses an upload with no Inbox", func() {
		ds.MockedProperty = &tests.MockedPropertyRepo{}
		_, _, err := upload("", "song.mp3", fixture("test.mp3"))
		Expect(err).To(MatchError(ErrNoInbox))
	})
})

// countingReader counts what was read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// endless is a body that never ends.
type endless struct{}

func (endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// onRead calls fn once, after the first `after` bytes were read.
type onRead struct {
	r     io.Reader
	n     int
	after int
	fn    func()
}

func (o *onRead) Read(p []byte) (int, error) {
	if o.fn != nil && o.n >= o.after {
		o.fn()
		o.fn = nil
	}
	n, err := o.r.Read(p)
	o.n += n
	return n, err
}

