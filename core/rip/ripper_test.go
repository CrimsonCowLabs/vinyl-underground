package rip

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeFetcher stands in for yt-dlp. Each Download writes a small File into the work folder, unless
// the URL has a hold, in which case it waits until the hold is released or its context ends.
type fakeFetcher struct {
	mu        sync.Mutex
	infos     map[string]*Info
	probeErr  map[string]error
	dlErr     map[string]error
	holds     map[string]chan struct{}
	started   chan string
	downloads []string
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{
		infos: map[string]*Info{}, probeErr: map[string]error{}, dlErr: map[string]error{},
		holds: map[string]chan struct{}{}, started: make(chan string, 10),
	}
}

func (f *fakeFetcher) hold(url string) chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan struct{})
	f.holds[url] = ch
	return ch
}

func (f *fakeFetcher) Probe(_ context.Context, url string) (*Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.probeErr[url]; err != nil {
		return nil, err
	}
	if info, ok := f.infos[url]; ok {
		return info, nil
	}
	return &Info{ID: "vid1", Title: "Song", Artist: "Artist", Source: "YouTube", SourceURL: url}, nil
}

func (f *fakeFetcher) Download(ctx context.Context, url, dir string) (string, error) {
	f.mu.Lock()
	f.downloads = append(f.downloads, url)
	hold := f.holds[url]
	err := f.dlErr[url]
	f.mu.Unlock()
	f.started <- url
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "vid1.opus")
	return path, os.WriteFile(path, []byte("audio"), 0o600)
}

func (f *fakeFetcher) downloaded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.downloads...)
}

// fakeScanner records the Inbox scans and what was in the Inbox when each one ran.
type fakeScanner struct {
	mu      sync.Mutex
	targets [][]model.ScanTarget
	inbox   string
	seen    [][]string
	err     error
	failN   int
	// blocked makes a scan hang until its context ends, like a long scan when the app stops.
	blocked bool
}

func (s *fakeScanner) ScanFolders(ctx context.Context, _ bool, targets []model.ScanTarget) ([]string, error) {
	s.mu.Lock()
	s.targets = append(s.targets, targets)
	s.seen = append(s.seen, visibleFiles(s.inbox))
	blocked := s.blocked
	s.mu.Unlock()
	if blocked {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failN > 0 {
		s.failN--
		return nil, scanner.ErrAlreadyScanning
	}
	return nil, s.err
}

func (s *fakeScanner) scans() [][]model.ScanTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]model.ScanTarget(nil), s.targets...)
}

// visibleFiles lists what the scanner would see in the Inbox: everything but hidden folders.
func visibleFiles(dir string) []string {
	var res []string
	entries, err := os.ReadDir(dir)
	Expect(err).ToNot(HaveOccurred())
	for _, e := range entries {
		if e.Name()[0] != '.' {
			res = append(res, e.Name())
		}
	}
	return res
}

var _ = Describe("Ripper", func() {
	var (
		ctx     context.Context
		ds      *tests.MockDataStore
		inbox   string
		fetcher *fakeFetcher
		scanner *fakeScanner
		tagged  []string
		ripper  *Ripper
		opts    Options
	)

	const inboxID = 2

	BeforeEach(func() {
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: "admin", IsAdmin: true})
		inbox = GinkgoT().TempDir()
		libs := &tests.MockLibraryRepo{}
		libs.SetData(model.Libraries{{ID: 1, Name: "Music", Path: GinkgoT().TempDir()}})
		libs.PutFn = func(l *model.Library) error {
			if l.ID == 0 {
				l.ID = inboxID
			}
			libs.Data[l.ID] = *l
			return nil
		}
		ds = &tests.MockDataStore{MockedLibrary: libs}
		_, err := core.EnsureInboxLibrary(ctx, ds, inbox)
		Expect(err).ToNot(HaveOccurred())

		fetcher = newFakeFetcher()
		scanner = &fakeScanner{inbox: inbox}
		tagged = nil
		opts = Options{
			Timeout:        time.Minute,
			ScanRetryDelay: time.Millisecond,
			Tagger: func(path string, info *Info) error {
				tagged = append(tagged, filepath.Base(path)+"|"+info.Source)
				return nil
			},
		}
	})

	start := func() (stop func()) {
		ripper = New(ds, fetcher, scanner, opts)
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			ripper.Run(runCtx)
		}()
		return func() {
			cancel()
			Eventually(done).Should(BeClosed())
		}
	}

	jobStatus := func(id string) func() model.RipStatus {
		return func() model.RipStatus {
			j, err := ds.RipJob(ctx).Get(id)
			Expect(err).ToNot(HaveOccurred())
			return j.Status
		}
	}

	getJob := func(id string) *model.RipJob {
		j, err := ds.RipJob(ctx).Get(id)
		Expect(err).ToNot(HaveOccurred())
		return j
	}

	Describe("Enqueue", func() {
		BeforeEach(func() { ripper = New(ds, fetcher, scanner, opts) })

		It("queues a job for the URL, owned by the admin", func() {
			job, err := ripper.Enqueue(ctx, "https://www.youtube.com/watch?v=abc")
			Expect(err).ToNot(HaveOccurred())
			Expect(job.Status).To(Equal(model.RipQueued))
			Expect(job.UserID).To(Equal("admin"))
			Expect(ripper.Jobs(ctx)).To(HaveLen(1))
		})

		DescribeTable("refuses anything but an http(s) URL",
			func(url string) {
				_, err := ripper.Enqueue(ctx, url)
				Expect(err).To(MatchError(ErrInvalidURL))
				Expect(ripper.Jobs(ctx)).To(BeEmpty())
			},
			Entry("empty", ""),
			Entry("no scheme", "www.youtube.com/watch?v=abc"),
			Entry("file", "file:///etc/passwd"),
			Entry("an option", "--exec=rm -rf /"),
			Entry("no host", "https:///watch"),
			Entry("javascript", "javascript:alert(1)"),
		)

		It("refuses when there is no Inbox", func() {
			ds.MockedLibrary.(*tests.MockLibraryRepo).SetData(model.Libraries{{ID: 1, Name: "Music", Path: "/music"}})
			_, err := ripper.Enqueue(ctx, "https://www.youtube.com/watch?v=abc")
			Expect(err).To(MatchError(ErrNoInbox))
		})
	})

	It("rips a video into the Inbox, tags it and scans it into a Track", func() {
		defer start()()
		job, err := ripper.Enqueue(ctx, "https://www.youtube.com/watch?v=abc")
		Expect(err).ToNot(HaveOccurred())

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		done := getJob(job.ID)
		Expect(done.Title).To(Equal("Song"))
		Expect(done.Source).To(Equal("YouTube"))
		Expect(done.Error).To(BeEmpty())
		Expect(done.Path).To(Equal("Artist - Song [vid1].opus"))

		Expect(filepath.Join(inbox, done.Path)).To(BeARegularFile())
		Expect(visibleFiles(inbox)).To(ConsistOf(done.Path))
		Expect(filepath.Join(inbox, ".rips", job.ID)).ToNot(BeAnExistingFile(), "the work folder is cleaned up")
		Expect(tagged).To(ConsistOf("vid1.opus|YouTube"), "tagged before it is moved into the Inbox")
		Expect(scanner.scans()).To(Equal([][]model.ScanTarget{{{LibraryID: inboxID}}}))
		Expect(scanner.seen).To(Equal([][]string{{done.Path}}), "the File is in place before the scan")
	})

	It("never shows the scanner a half-written File", func() {
		hold := fetcher.hold("https://example.com/slow")
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/slow")

		Eventually(fetcher.started).Should(Receive())
		Expect(jobStatus(job.ID)()).To(Equal(model.RipDownloading))
		Expect(visibleFiles(inbox)).To(BeEmpty())
		close(hold)
		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
	})

	It("doesn't overwrite a File already in the Inbox", func() {
		Expect(os.WriteFile(filepath.Join(inbox, "Artist - Song [vid1].opus"), []byte("older"), 0o600)).To(Succeed())
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a")

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(getJob(job.ID).Path).To(Equal("Artist - Song [vid1] (2).opus"))
		Expect(os.ReadFile(filepath.Join(inbox, "Artist - Song [vid1].opus"))).To(Equal([]byte("older")))
	})

	It("makes a safe file name from the page's title", func() {
		fetcher.infos["https://example.com/a"] = &Info{ID: "x/y", Title: "../../etc/passwd", Artist: ".hidden", Source: "Site", SourceURL: "https://example.com/a"}
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a")

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		p := getJob(job.ID).Path
		Expect(filepath.Dir(p)).To(Equal("."))
		Expect(p).ToNot(HavePrefix("."))
		Expect(filepath.Join(inbox, p)).To(BeARegularFile())
	})

	It("runs one job at a time, in the order they were queued", func() {
		hold := fetcher.hold("https://example.com/first")
		defer start()()
		first, _ := ripper.Enqueue(ctx, "https://example.com/first")
		second, _ := ripper.Enqueue(ctx, "https://example.com/second")

		Eventually(fetcher.started).Should(Receive(Equal("https://example.com/first")))
		Consistently(jobStatus(second.ID), 100*time.Millisecond).Should(Equal(model.RipQueued))
		close(hold)

		Eventually(jobStatus(second.ID)).Should(Equal(model.RipReady))
		Expect(jobStatus(first.ID)()).To(Equal(model.RipReady))
		Expect(fetcher.downloaded()).To(Equal([]string{"https://example.com/first", "https://example.com/second"}))
	})

	It("fails with yt-dlp's error message", func() {
		fetcher.dlErr["https://example.com/gone"] = errors.New("ERROR: [youtube] abc: Video unavailable")
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/gone")

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipFailed))
		Expect(getJob(job.ID).Error).To(Equal("ERROR: [youtube] abc: Video unavailable"))
		Expect(visibleFiles(inbox)).To(BeEmpty())
		Expect(filepath.Join(inbox, ".rips", job.ID)).ToNot(BeAnExistingFile())
	})

	It("keeps going after a failed job", func() {
		fetcher.probeErr["https://example.com/bad"] = errors.New("ERROR: Unsupported URL")
		defer start()()
		bad, _ := ripper.Enqueue(ctx, "https://example.com/bad")
		good, _ := ripper.Enqueue(ctx, "https://example.com/good")

		Eventually(jobStatus(good.ID)).Should(Equal(model.RipReady))
		Expect(getJob(bad.ID).Error).To(Equal("ERROR: Unsupported URL"))
	})

	It("refuses a playlist, as only single videos can be ripped for now", func() {
		fetcher.infos["https://www.youtube.com/playlist?list=PL1"] = &Info{ID: "PL1", Title: "A Playlist", IsPlaylist: true}
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://www.youtube.com/playlist?list=PL1")

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipFailed))
		Expect(getJob(job.ID).Error).To(ContainSubstring("playlist"))
		Expect(fetcher.downloaded()).To(BeEmpty())
	})

	It("fails a job that runs past the timeout", func() {
		opts.Timeout = 100 * time.Millisecond
		fetcher.hold("https://example.com/stuck")
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/stuck")

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipFailed))
		Expect(getJob(job.ID).Error).To(ContainSubstring("took longer than 100ms"))
	})

	It("keeps a job going when the request that started it is gone", func() {
		hold := fetcher.hold("https://example.com/a")
		defer start()()
		reqCtx, cancelReq := context.WithCancel(ctx)
		job, _ := ripper.Enqueue(reqCtx, "https://example.com/a")
		cancelReq()

		Eventually(fetcher.started).Should(Receive())
		close(hold)
		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
	})

	It("picks up a job queued before the app started", func() {
		ripper = New(ds, fetcher, scanner, opts)
		job, _ := ripper.Enqueue(ctx, "https://example.com/a")
		defer start()()

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
	})

	It("re-queues a job the app stopped in the middle of, and finishes it on the next start", func() {
		hold := fetcher.hold("https://example.com/a")
		stop := start()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a")
		Eventually(fetcher.started).Should(Receive())
		stop()
		Expect(jobStatus(job.ID)()).To(Equal(model.RipDownloading), "an interrupted job isn't marked failed")
		Expect(filepath.Join(inbox, ".rips", job.ID)).ToNot(BeAnExistingFile())

		close(hold)
		defer start()()
		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(visibleFiles(inbox)).To(HaveLen(1))
	})

	It("retries the Inbox scan while another scan is running", func() {
		scanner.failN = 2
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a")

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(scanner.scans()).To(HaveLen(3))
		Expect(getJob(job.ID).Error).To(BeEmpty())
	})

	It("still reports the Rip as ready, with a note, if the Inbox scan fails", func() {
		scanner.err = errors.New("disk on fire")
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a")

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(getJob(job.ID).Error).To(ContainSubstring("disk on fire"))
		Expect(visibleFiles(inbox)).To(HaveLen(1))
		Expect(scanner.scans()).To(HaveLen(1), "only a busy scanner is retried")
	})

	It("doesn't fail a Rip whose timeout passes while it waits for the Inbox scan", func() {
		opts.Timeout = 100 * time.Millisecond
		opts.ScanRetryDelay = 50 * time.Millisecond
		scanner.failN = 4
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a")

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(getJob(job.ID).Error).To(BeEmpty())
	})

	It("only scans, without ripping again, a job the app stopped after its File landed", func() {
		scanner.blocked = true
		stop := start()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a")
		Eventually(func() int { return len(scanner.scans()) }).Should(Equal(1))
		stop()
		Expect(jobStatus(job.ID)()).To(Equal(model.RipDownloading))
		Expect(getJob(job.ID).Path).ToNot(BeEmpty())

		scanner.mu.Lock()
		scanner.blocked = false
		scanner.mu.Unlock()
		defer start()()
		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(fetcher.downloaded()).To(HaveLen(1))
		Expect(visibleFiles(inbox)).To(HaveLen(1))
	})
})
