package rip

import (
	"context"
	"errors"
	"fmt"
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

	// getJob reads the job, with its items, as the API lists it.
	getJob := func(id string) *model.RipJob {
		jobs, err := New(ds, fetcher, scanner, opts).Jobs(ctx)
		Expect(err).ToNot(HaveOccurred())
		for _, j := range jobs {
			if j.ID == id {
				return &j
			}
		}
		Fail("no job " + id)
		return nil
	}

	// fileOf is where a single video's File landed, relative to the Inbox.
	fileOf := func(id string) string {
		items := getJob(id).Items
		Expect(items).To(HaveLen(1))
		return items[0].Path
	}

	itemStatuses := func(id string) func() []model.RipStatus {
		return func() []model.RipStatus {
			var res []model.RipStatus
			for _, it := range getJob(id).Items {
				res = append(res, it.Status)
			}
			return res
		}
	}

	Describe("Enqueue", func() {
		BeforeEach(func() { ripper = New(ds, fetcher, scanner, opts) })

		It("queues a job for the URL, owned by the admin", func() {
			job, err := ripper.Enqueue(ctx, "https://www.youtube.com/watch?v=abc", false)
			Expect(err).ToNot(HaveOccurred())
			Expect(job.Status).To(Equal(model.RipQueued))
			Expect(job.UserID).To(Equal("admin"))
			Expect(ripper.Jobs(ctx)).To(HaveLen(1))
		})

		DescribeTable("refuses anything but an http(s) URL",
			func(url string) {
				_, err := ripper.Enqueue(ctx, url, false)
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
			_, err := ripper.Enqueue(ctx, "https://www.youtube.com/watch?v=abc", false)
			Expect(err).To(MatchError(ErrNoInbox))
		})
	})

	It("rips a video into the Inbox, tags it and scans it into a Track", func() {
		defer start()()
		job, err := ripper.Enqueue(ctx, "https://www.youtube.com/watch?v=abc", false)
		Expect(err).ToNot(HaveOccurred())

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		done := getJob(job.ID)
		Expect(done.Title).To(Equal("Song"))
		Expect(done.Source).To(Equal("YouTube"))
		Expect(done.Error).To(BeEmpty())
		Expect(fileOf(job.ID)).To(Equal("Artist - Song [vid1].opus"))

		Expect(filepath.Join(inbox, fileOf(job.ID))).To(BeARegularFile())
		Expect(visibleFiles(inbox)).To(ConsistOf(fileOf(job.ID)))
		Expect(filepath.Join(inbox, ".rips")).ToNot(BeAnExistingFile(), "the work folder is cleaned up")
		Expect(tagged).To(ConsistOf("vid1.opus|YouTube"), "tagged before it is moved into the Inbox")
		Expect(scanner.scans()).To(Equal([][]model.ScanTarget{{{LibraryID: inboxID}}}))
		Expect(scanner.seen).To(Equal([][]string{{fileOf(job.ID)}}), "the File is in place before the scan")
	})

	It("never shows the scanner a half-written File", func() {
		hold := fetcher.hold("https://example.com/slow")
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/slow", false)

		Eventually(fetcher.started).Should(Receive())
		Expect(jobStatus(job.ID)()).To(Equal(model.RipDownloading))
		Expect(visibleFiles(inbox)).To(BeEmpty())
		close(hold)
		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
	})

	It("doesn't overwrite a File already in the Inbox", func() {
		Expect(os.WriteFile(filepath.Join(inbox, "Artist - Song [vid1].opus"), []byte("older"), 0o600)).To(Succeed())
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a", false)

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(fileOf(job.ID)).To(Equal("Artist - Song [vid1] (2).opus"))
		Expect(os.ReadFile(filepath.Join(inbox, "Artist - Song [vid1].opus"))).To(Equal([]byte("older")))
	})

	It("makes a safe file name from the page's title", func() {
		fetcher.infos["https://example.com/a"] = &Info{ID: "x/y", Title: "../../etc/passwd", Artist: ".hidden", Source: "Site", SourceURL: "https://example.com/a"}
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a", false)

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		p := fileOf(job.ID)
		Expect(filepath.Dir(p)).To(Equal("."))
		Expect(p).ToNot(HavePrefix("."))
		Expect(filepath.Join(inbox, p)).To(BeARegularFile())
	})

	It("runs one job at a time, in the order they were queued", func() {
		hold := fetcher.hold("https://example.com/first")
		defer start()()
		first, _ := ripper.Enqueue(ctx, "https://example.com/first", false)
		second, _ := ripper.Enqueue(ctx, "https://example.com/second", false)

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
		job, _ := ripper.Enqueue(ctx, "https://example.com/gone", false)

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipFailed))
		Expect(getJob(job.ID).Error).To(Equal("ERROR: [youtube] abc: Video unavailable"))
		Expect(visibleFiles(inbox)).To(BeEmpty())
		Expect(filepath.Join(inbox, ".rips")).ToNot(BeAnExistingFile())
	})

	It("keeps going after a failed job", func() {
		fetcher.probeErr["https://example.com/bad"] = errors.New("ERROR: Unsupported URL")
		defer start()()
		bad, _ := ripper.Enqueue(ctx, "https://example.com/bad", false)
		good, _ := ripper.Enqueue(ctx, "https://example.com/good", false)

		Eventually(jobStatus(good.ID)).Should(Equal(model.RipReady))
		Expect(getJob(bad.ID).Error).To(Equal("ERROR: Unsupported URL"))
	})

	Describe("playlists and albums", func() {
		const listURL = "https://music.youtube.com/playlist?list=OLAK5uy_x"

		// playlist makes the fake list n items at listURL, each with its own page and metadata.
		playlist := func(title string, n int) []string {
			info := &Info{ID: "OLAK5uy_x", Title: title, Source: "YouTube", SourceURL: listURL, IsPlaylist: true, Count: n}
			var urls []string
			for i := 1; i <= n; i++ {
				u := fmt.Sprintf("https://music.youtube.com/watch?v=t%d", i)
				urls = append(urls, u)
				info.Entries = append(info.Entries, PlaylistEntry{URL: u, Title: fmt.Sprintf("Listed %d", i), Artist: "Band"})
				fetcher.infos[u] = &Info{ID: fmt.Sprintf("t%d", i), Title: fmt.Sprintf("Track %d", i), Artist: "Band", Source: "YouTube", SourceURL: u}
			}
			fetcher.infos[listURL] = info
			return urls
		}

		It("rips every item into one job, one File each, in a folder named after the album", func() {
			urls := playlist("Great Album", 3)
			defer start()()
			job, err := ripper.Enqueue(ctx, listURL, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(job.Items).To(HaveLen(3), "the items are known as soon as the job is queued")
			Expect(job.Items[0].Title).To(Equal("Listed 1"))

			Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
			done := getJob(job.ID)
			Expect(done.Title).To(Equal("Great Album"))
			Expect(done.Folder).To(Equal("Great Album"))
			Expect(done.Error).To(BeEmpty())
			var paths []string
			for i, it := range done.Items {
				Expect(it.Position).To(Equal(i + 1))
				Expect(it.URL).To(Equal(urls[i]))
				Expect(it.Status).To(Equal(model.RipReady))
				Expect(it.Title).To(Equal(fmt.Sprintf("Track %d", i+1)), "the item's own metadata replaces the listing's")
				Expect(it.Artist).To(Equal("Band"))
				Expect(filepath.Join(inbox, it.Path)).To(BeARegularFile())
				paths = append(paths, it.Path)
			}
			Expect(paths).To(Equal([]string{
				"Great Album/Band - Track 1 [t1].opus",
				"Great Album/Band - Track 2 [t2].opus",
				"Great Album/Band - Track 3 [t3].opus",
			}))
			Expect(fetcher.downloaded()).To(Equal(urls), "each item is ripped on its own, in order")
			Expect(scanner.scans()).To(HaveLen(1), "the Inbox is scanned once, when the job is done")
			Expect(visibleFiles(inbox)).To(ConsistOf("Great Album"))
		})

		It("shows each item's progress while the job runs", func() {
			urls := playlist("Album", 3)
			hold := fetcher.hold(urls[1])
			defer start()()
			job, _ := ripper.Enqueue(ctx, listURL, false)

			Eventually(fetcher.started).Should(Receive(Equal(urls[0])))
			Eventually(fetcher.started).Should(Receive(Equal(urls[1])))
			Eventually(itemStatuses(job.ID)).Should(Equal([]model.RipStatus{model.RipReady, model.RipDownloading, model.RipQueued}))
			Expect(jobStatus(job.ID)()).To(Equal(model.RipDownloading))
			close(hold)
			Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		})

		It("doesn't fail the job for one failed item: the job is ready with failures, the item shows why", func() {
			urls := playlist("Album", 3)
			fetcher.dlErr[urls[1]] = errors.New("ERROR: [youtube] t2: Video unavailable")
			defer start()()
			job, _ := ripper.Enqueue(ctx, listURL, false)

			Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
			done := getJob(job.ID)
			Expect(done.Error).To(Equal("1 of 3 Tracks failed"))
			Expect(itemStatuses(job.ID)()).To(Equal([]model.RipStatus{model.RipReady, model.RipFailed, model.RipReady}))
			Expect(done.Items[1].Error).To(Equal("ERROR: [youtube] t2: Video unavailable"))
			Expect(done.Items[1].Path).To(BeEmpty())
			Expect(fetcher.downloaded()).To(Equal(urls), "the items after the failed one are still ripped")
			Expect(scanner.scans()).To(HaveLen(1))
		})

		It("fails the job when none of its items could be ripped", func() {
			urls := playlist("Album", 2)
			for _, u := range urls {
				fetcher.dlErr[u] = errors.New("ERROR: Video unavailable")
			}
			defer start()()
			job, _ := ripper.Enqueue(ctx, listURL, false)

			Eventually(jobStatus(job.ID)).Should(Equal(model.RipFailed))
			Expect(getJob(job.ID).Error).To(Equal("none of the 2 Tracks could be ripped"))
			Expect(scanner.scans()).To(BeEmpty(), "nothing landed, so there is nothing to scan")
		})

		It("fails an item that has no web page of its own, and rips the rest", func() {
			playlist("Album", 2)
			fetcher.infos[listURL].Entries[0].URL = "t1"
			defer start()()
			job, _ := ripper.Enqueue(ctx, listURL, false)

			Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
			Expect(itemStatuses(job.ID)()).To(Equal([]model.RipStatus{model.RipFailed, model.RipReady}))
			Expect(fetcher.downloaded()).To(HaveLen(1))
		})

		It("fails an item that turns out to be a playlist itself", func() {
			urls := playlist("Channel", 2)
			fetcher.infos[urls[0]] = &Info{ID: "PL2", Title: "Uploads", IsPlaylist: true, Count: 9}
			defer start()()
			job, _ := ripper.Enqueue(ctx, listURL, false)

			Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
			Expect(getJob(job.ID).Items[0].Error).To(ContainSubstring("playlist"))
			Expect(fetcher.downloaded()).To(Equal(urls[1:]))
		})

		It("gives each job a folder of its own when the same album is ripped again", func() {
			playlist("Great Album", 2)
			defer start()()
			first, _ := ripper.Enqueue(ctx, listURL, false)
			Eventually(jobStatus(first.ID)).Should(Equal(model.RipReady))
			second, _ := ripper.Enqueue(ctx, listURL, false)
			Eventually(jobStatus(second.ID)).Should(Equal(model.RipReady))

			Expect(getJob(second.ID).Folder).To(Equal("Great Album (2)"))
			Expect(getJob(second.ID).Items[0].Path).To(Equal("Great Album (2)/Band - Track 1 [t1].opus"))
			Expect(visibleFiles(filepath.Join(inbox, "Great Album"))).To(HaveLen(2))
			Expect(visibleFiles(filepath.Join(inbox, "Great Album (2)"))).To(HaveLen(2))
		})

		It("names the folder safely, and falls back to the playlist's id", func() {
			playlist("../../etc", 1)
			defer start()()
			job, _ := ripper.Enqueue(ctx, listURL, false)
			Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
			Expect(getJob(job.ID).Folder).To(Equal("_.._etc"))

			playlist(" .. ", 1)
			job, _ = ripper.Enqueue(ctx, listURL, false)
			Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
			Expect(getJob(job.ID).Folder).To(Equal("OLAK5uy_x"))
		})

		It("rips up to 50 items without asking", func() {
			playlist("Album", ConfirmAbove)
			ripper = New(ds, fetcher, scanner, opts)
			job, err := ripper.Enqueue(ctx, listURL, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(job.Items).To(HaveLen(50))
		})

		It("asks for confirmation above 50 items, and queues nothing until it is given", func() {
			playlist("Big Playlist", ConfirmAbove+1)
			ripper = New(ds, fetcher, scanner, opts)

			_, err := ripper.Enqueue(ctx, listURL, false)
			var confirm *NeedsConfirmationError
			Expect(errors.As(err, &confirm)).To(BeTrue())
			Expect(confirm.Preview.Count).To(Equal(51))
			Expect(confirm.Preview.Title).To(Equal("Big Playlist"))
			Expect(confirm.Preview.Titles).To(HaveLen(51))
			Expect(confirm.Preview.Titles[0]).To(Equal("Band - Listed 1"))
			Expect(err.Error()).To(ContainSubstring("51"))
			Expect(ripper.Jobs(ctx)).To(BeEmpty())

			job, err := ripper.Enqueue(ctx, listURL, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(job.Items).To(HaveLen(51))
		})

		It("refuses more than 300 items, even when confirmed", func() {
			fetcher.infos[listURL] = &Info{ID: "PL", Title: "Everything", IsPlaylist: true, Count: 1200,
				Entries: make([]PlaylistEntry, MaxItems+1)}
			ripper = New(ds, fetcher, scanner, opts)

			_, err := ripper.Enqueue(ctx, listURL, true)
			var tooMany *TooManyItemsError
			Expect(errors.As(err, &tooMany)).To(BeTrue())
			Expect(err.Error()).To(Equal("this playlist has 1200 items, and at most 300 can be ripped at once"))
			Expect(ripper.Jobs(ctx)).To(BeEmpty())
		})

		It("says \"more than 300\" when the site gives no total, as only 301 items are listed", func() {
			fetcher.infos[listURL] = &Info{ID: "UC", Title: "Channel", IsPlaylist: true, Count: MaxItems + 1,
				Entries: make([]PlaylistEntry, MaxItems+1)}
			ripper = New(ds, fetcher, scanner, opts)
			_, err := ripper.Enqueue(ctx, listURL, false)
			Expect(err).To(MatchError("this playlist has more than 300 items, and at most 300 can be ripped at once"))
		})

		It("fails a playlist with nothing in it", func() {
			fetcher.infos[listURL] = &Info{ID: "PL", Title: "Empty", IsPlaylist: true}
			ripper = New(ds, fetcher, scanner, opts)
			job, err := ripper.Enqueue(ctx, listURL, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(job.Status).To(Equal(model.RipFailed))
			Expect(job.Error).To(ContainSubstring("empty"))
		})

		It("after a restart, re-queues the item that was downloading and doesn't rip the landed ones again", func() {
			urls := playlist("Album", 3)
			hold := fetcher.hold(urls[1])
			stop := start()
			job, _ := ripper.Enqueue(ctx, listURL, false)
			Eventually(fetcher.started).Should(Receive(Equal(urls[0])))
			Eventually(fetcher.started).Should(Receive(Equal(urls[1])))
			stop()
			Expect(itemStatuses(job.ID)()).To(Equal([]model.RipStatus{model.RipReady, model.RipDownloading, model.RipQueued}))

			close(hold)
			defer start()()
			Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
			Expect(fetcher.downloaded()).To(Equal([]string{urls[0], urls[1], urls[1], urls[2]}))
			Expect(itemStatuses(job.ID)()).To(Equal([]model.RipStatus{model.RipReady, model.RipReady, model.RipReady}))
			Expect(visibleFiles(filepath.Join(inbox, "Album"))).To(HaveLen(3))
		})
	})

	It("fails a job that runs past the timeout", func() {
		opts.Timeout = 100 * time.Millisecond
		fetcher.hold("https://example.com/stuck")
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/stuck", false)

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipFailed))
		Expect(getJob(job.ID).Error).To(ContainSubstring("took longer than 100ms"))
	})

	It("keeps a job going when the request that started it is gone", func() {
		hold := fetcher.hold("https://example.com/a")
		defer start()()
		reqCtx, cancelReq := context.WithCancel(ctx)
		job, _ := ripper.Enqueue(reqCtx, "https://example.com/a", false)
		cancelReq()

		Eventually(fetcher.started).Should(Receive())
		close(hold)
		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
	})

	It("picks up a job queued before the app started", func() {
		ripper = New(ds, fetcher, scanner, opts)
		job, _ := ripper.Enqueue(ctx, "https://example.com/a", false)
		defer start()()

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
	})

	It("re-queues a job the app stopped in the middle of, and finishes it on the next start", func() {
		hold := fetcher.hold("https://example.com/a")
		stop := start()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a", false)
		Eventually(fetcher.started).Should(Receive())
		stop()
		Expect(jobStatus(job.ID)()).To(Equal(model.RipDownloading), "an interrupted job isn't marked failed")
		Expect(filepath.Join(inbox, ".rips")).ToNot(BeAnExistingFile())

		close(hold)
		defer start()()
		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(visibleFiles(inbox)).To(HaveLen(1))
	})

	It("retries the Inbox scan while another scan is running", func() {
		scanner.failN = 2
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a", false)

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(scanner.scans()).To(HaveLen(3))
		Expect(getJob(job.ID).Error).To(BeEmpty())
	})

	It("still reports the Rip as ready, with a note, if the Inbox scan fails", func() {
		scanner.err = errors.New("disk on fire")
		defer start()()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a", false)

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
		job, _ := ripper.Enqueue(ctx, "https://example.com/a", false)

		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(getJob(job.ID).Error).To(BeEmpty())
	})

	It("only scans, without ripping again, a job the app stopped after its File landed", func() {
		scanner.blocked = true
		stop := start()
		job, _ := ripper.Enqueue(ctx, "https://example.com/a", false)
		Eventually(func() int { return len(scanner.scans()) }).Should(Equal(1))
		stop()
		Expect(jobStatus(job.ID)()).To(Equal(model.RipDownloading))
		Expect(fileOf(job.ID)).ToNot(BeEmpty())

		scanner.mu.Lock()
		scanner.blocked = false
		scanner.mu.Unlock()
		defer start()()
		Eventually(jobStatus(job.ID)).Should(Equal(model.RipReady))
		Expect(fetcher.downloaded()).To(HaveLen(1))
		Expect(visibleFiles(inbox)).To(HaveLen(1))
	})
})

// fakeUpdater stands in for yt-dlp's updates. onUpdate runs on each update, e.g. to make the
// fetcher work again as a newer yt-dlp would.
type fakeUpdater struct {
	mu       sync.Mutex
	calls    int
	err      error
	onUpdate func()
}

func (u *fakeUpdater) Update(context.Context) (UpdateResult, error) {
	u.mu.Lock()
	u.calls++
	on, err := u.onUpdate, u.err
	u.mu.Unlock()
	if on != nil {
		on()
	}
	if err != nil {
		return UpdateResult{Previous: "2026.01.01", Version: "2026.01.01"}, err
	}
	return UpdateResult{Previous: "2026.01.01", Version: "2026.11.01", Updated: true}, nil
}

func (u *fakeUpdater) Version(context.Context) (string, error) { return "2026.01.01", nil }

func (u *fakeUpdater) updates() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

// fakeScheduler records what is scheduled, so a test can run it.
type fakeScheduler struct {
	crontabs []string
	cmds     []func()
}

func (s *fakeScheduler) Add(crontab string, cmd func()) (int, error) {
	s.crontabs = append(s.crontabs, crontab)
	s.cmds = append(s.cmds, cmd)
	return len(s.cmds), nil
}

var _ = Describe("Ripper and yt-dlp updates", func() {
	var (
		ctx     context.Context
		ds      *tests.MockDataStore
		fetcher *fakeFetcher
		updater *fakeUpdater
		opts    Options
		ripper  *Ripper
		inbox   string
	)

	const url = "https://www.youtube.com/watch?v=abc"
	botCheck := &YtDlpError{Msg: "ERROR: [youtube] abc: Sign in to confirm you're not a bot"}

	BeforeEach(func() {
		ctx = request.WithUser(GinkgoT().Context(), model.User{ID: "admin", IsAdmin: true})
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
		inbox = GinkgoT().TempDir()
		_, err := core.EnsureInboxLibrary(ctx, ds, inbox)
		Expect(err).ToNot(HaveOccurred())
		fetcher = newFakeFetcher()
		updater = &fakeUpdater{}
		opts = Options{
			Timeout: time.Minute, ScanRetryDelay: time.Millisecond, Updater: updater,
			Tagger: func(string, *Info) error { return nil },
		}
	})

	start := func() {
		ripper = New(ds, fetcher, &fakeScanner{inbox: inbox}, opts)
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			ripper.Run(runCtx)
		}()
		DeferCleanup(func() {
			cancel()
			Eventually(done).Should(BeClosed())
		})
	}

	finished := func(id string) func() *model.RipJob {
		return func() *model.RipJob {
			j, err := ds.RipJob(ctx).Get(id)
			Expect(err).ToNot(HaveOccurred())
			if j.Status != model.RipReady && j.Status != model.RipFailed {
				return nil
			}
			return j
		}
	}

	Describe("after a failed Rip", func() {
		It("updates yt-dlp and tries the Rip once more", func() {
			fetcher.dlErr[url] = botCheck
			updater.onUpdate = func() {
				fetcher.mu.Lock()
				delete(fetcher.dlErr, url)
				fetcher.mu.Unlock()
			}
			start()
			job, _ := ripper.Enqueue(ctx, url, false)

			Eventually(finished(job.ID)).Should(HaveField("Status", model.RipReady))
			Expect(finished(job.ID)().Error).To(BeEmpty())
			Expect(updater.updates()).To(Equal(1))
			Expect(fetcher.downloaded()).To(HaveLen(2))
		})

		It("fails the job when the retry fails too, without trying again", func() {
			fetcher.dlErr[url] = botCheck
			start()
			job, _ := ripper.Enqueue(ctx, url, false)

			Eventually(finished(job.ID)).Should(HaveField("Status", model.RipFailed))
			Expect(finished(job.ID)().Error).To(Equal(botCheck.Msg))
			Expect(updater.updates()).To(Equal(1))
			Expect(fetcher.downloaded()).To(HaveLen(2))
		})

		It("still retries with the old yt-dlp when the update fails, and says so", func() {
			fetcher.dlErr[url] = botCheck
			updater.err = errors.New("GitHub is down")
			start()
			job, _ := ripper.Enqueue(ctx, url, false)

			Eventually(finished(job.ID)).Should(HaveField("Status", model.RipFailed))
			Expect(finished(job.ID)().Error).To(And(ContainSubstring(botCheck.Msg), ContainSubstring("GitHub is down")))
			Expect(fetcher.downloaded()).To(HaveLen(2))
		})

		It("updates and retries for each failed job", func() {
			fetcher.dlErr[url] = botCheck
			start()
			first, _ := ripper.Enqueue(ctx, url, false)
			second, _ := ripper.Enqueue(ctx, url, false)

			Eventually(finished(second.ID)).ShouldNot(BeNil())
			Expect(finished(first.ID)()).ToNot(BeNil())
			Expect(updater.updates()).To(Equal(2))
		})

		It("updates and retries only the failed item of a playlist", func() {
			const list = "https://www.youtube.com/playlist?list=PL1"
			info := &Info{ID: "PL1", Title: "Album", IsPlaylist: true, Count: 3}
			for _, v := range []string{"a", "b", "c"} {
				info.Entries = append(info.Entries, PlaylistEntry{URL: "https://www.youtube.com/watch?v=" + v, Title: v})
			}
			fetcher.infos[list] = info
			second := info.Entries[1].URL
			fetcher.dlErr[second] = botCheck
			updater.onUpdate = func() {
				fetcher.mu.Lock()
				delete(fetcher.dlErr, second)
				fetcher.mu.Unlock()
			}
			start()
			job, _ := ripper.Enqueue(ctx, list, false)

			Eventually(finished(job.ID)).Should(HaveField("Status", model.RipReady))
			Expect(finished(job.ID)().Error).To(BeEmpty())
			Expect(updater.updates()).To(Equal(1))
			Expect(fetcher.downloaded()).To(Equal([]string{info.Entries[0].URL, second, second, info.Entries[2].URL}))
		})

		It("queues a page yt-dlp couldn't read as one video, and updates and retries it in the worker", func() {
			fetcher.probeErr[url] = botCheck
			updater.onUpdate = func() {
				fetcher.mu.Lock()
				delete(fetcher.probeErr, url)
				fetcher.mu.Unlock()
			}
			start()
			job, err := ripper.Enqueue(ctx, url, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(job.Status).To(Equal(model.RipQueued))
			Expect(job.Items).To(HaveLen(1))

			Eventually(finished(job.ID)).Should(HaveField("Status", model.RipReady))
			Expect(updater.updates()).To(Equal(1))
		})

		It("asks for the link again when a page queued that way turns out to be a playlist", func() {
			fetcher.probeErr[url] = botCheck
			updater.onUpdate = func() {
				fetcher.mu.Lock()
				delete(fetcher.probeErr, url)
				fetcher.infos[url] = &Info{ID: "PL1", Title: "A Playlist", IsPlaylist: true, Count: 2}
				fetcher.mu.Unlock()
			}
			start()
			job, _ := ripper.Enqueue(ctx, url, false)

			Eventually(finished(job.ID)).Should(HaveField("Status", model.RipFailed))
			Expect(finished(job.ID)().Error).To(ContainSubstring("paste the link again"))
			Expect(fetcher.downloaded()).To(BeEmpty())
		})

		DescribeTable("doesn't update or retry what an update can't fix",
			func(setup func(), wantErr string) {
				setup()
				start()
				job, _ := ripper.Enqueue(ctx, url, false)

				Eventually(finished(job.ID)).Should(HaveField("Status", model.RipFailed))
				Expect(finished(job.ID)().Error).To(ContainSubstring(wantErr))
				Expect(updater.updates()).To(BeZero())
				Expect(len(fetcher.downloaded())).To(BeNumerically("<=", 1))
			},
			Entry("an unsupported URL", func() {
				fetcher.probeErr[url] = &YtDlpError{Msg: "ERROR: Unsupported URL: https://example.com/"}
			}, "Unsupported URL"),
			Entry("a private video", func() {
				fetcher.dlErr[url] = &YtDlpError{Msg: "ERROR: [youtube] abc: Private video. Sign in if you've been granted access to this video"}
			}, "Private video"),
			Entry("a removed video", func() {
				fetcher.dlErr[url] = &YtDlpError{Msg: "ERROR: [youtube] abc: Video unavailable. This video has been removed by the uploader"}
			}, "removed"),
			Entry("a video taken down", func() {
				fetcher.probeErr[url] = &YtDlpError{Msg: "ERROR: [youtube] abc: This video is unavailable"}
			}, "unavailable"),
			Entry("a playlist", func() {
				fetcher.infos[url] = &Info{ID: "PL1", Title: "A Playlist", IsPlaylist: true}
			}, "playlist"),
			Entry("the Rip's own timeout", func() {
				opts.Timeout = 100 * time.Millisecond
				fetcher.hold(url)
			}, "took longer than"),
			Entry("an error that isn't yt-dlp's", func() {
				opts.Tagger = func(string, *Info) error { return errors.New("disk full") }
			}, "disk full"),
		)
	})

	Describe("UpdateYtDlp", func() {
		It("waits for the running Rip before it touches yt-dlp", func() {
			hold := fetcher.hold(url)
			start()
			job, _ := ripper.Enqueue(ctx, url, false)
			Eventually(fetcher.started).Should(Receive())

			done := make(chan error, 1)
			go func() {
				_, err := ripper.UpdateYtDlp(ctx, true)
				done <- err
			}()
			Consistently(updater.updates, 100*time.Millisecond).Should(BeZero())
			close(hold)
			Eventually(done).Should(Receive(BeNil()))
			Expect(updater.updates()).To(Equal(1))
			Eventually(finished(job.ID)).Should(HaveField("Status", model.RipReady))
		})

		It("runs between two items of a playlist, rather than after the whole job", func() {
			const list = "https://www.youtube.com/playlist?list=PL1"
			info := &Info{ID: "PL1", Title: "Album", IsPlaylist: true, Count: 2}
			for _, v := range []string{"a", "b"} {
				info.Entries = append(info.Entries, PlaylistEntry{URL: "https://www.youtube.com/watch?v=" + v, Title: v})
			}
			fetcher.infos[list] = info
			hold := fetcher.hold(info.Entries[0].URL)
			var downloadedAtUpdate []string
			updater.onUpdate = func() { downloadedAtUpdate = fetcher.downloaded() }
			start()
			job, _ := ripper.Enqueue(ctx, list, false)
			Eventually(fetcher.started).Should(Receive())

			done := make(chan error, 1)
			go func() {
				_, err := ripper.UpdateYtDlp(ctx, true)
				done <- err
			}()
			Consistently(updater.updates, 100*time.Millisecond).Should(BeZero())
			close(hold)
			Eventually(done).Should(Receive(BeNil()))
			Expect(downloadedAtUpdate).To(Equal([]string{info.Entries[0].URL}), "the update ran before the second item")
			Eventually(finished(job.ID)).Should(HaveField("Status", model.RipReady))
		})

		It("leaves a job queued, not downloading, while an update holds yt-dlp", func() {
			block := make(chan struct{})
			updater.onUpdate = func() { <-block }
			start()
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = ripper.UpdateYtDlp(ctx, true)
			}()
			Eventually(updater.updates).Should(Equal(1))

			job, _ := ripper.Enqueue(ctx, url, false)
			status := func() []model.RipStatus {
				jobs, err := ripper.Jobs(ctx)
				Expect(err).ToNot(HaveOccurred())
				return []model.RipStatus{jobs[0].Status, jobs[0].Items[0].Status}
			}
			Consistently(status, 200*time.Millisecond).Should(Equal([]model.RipStatus{model.RipQueued, model.RipQueued}))
			close(block)
			Eventually(done).Should(BeClosed())
			Eventually(finished(job.ID)).Should(HaveField("Status", model.RipReady))
		})

		It("refuses straight away, when asked not to wait, while a Rip is running", func() {
			hold := fetcher.hold(url)
			start()
			_, _ = ripper.Enqueue(ctx, url, false)
			Eventually(fetcher.started).Should(Receive())

			_, err := ripper.UpdateYtDlp(ctx, false)
			Expect(err).To(MatchError(ErrRipRunning))
			Expect(updater.updates()).To(BeZero())
			close(hold)
		})

		It("refuses straight away, when asked not to wait, while another update runs", func() {
			block := make(chan struct{})
			updater.onUpdate = func() { <-block }
			start()
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = ripper.UpdateYtDlp(ctx, true)
			}()
			Eventually(updater.updates).Should(Equal(1))

			_, err := ripper.UpdateYtDlp(ctx, false)
			Expect(err).To(MatchError(ErrUpdateRunning))
			close(block)
			Eventually(done).Should(BeClosed())
		})

		It("can't update without an updater", func() {
			opts.Updater = nil
			start()
			_, err := ripper.UpdateYtDlp(ctx, false)
			Expect(err).To(MatchError(ErrNoUpdater))
		})

		It("updates straight away when no Rip is running", func() {
			start()
			res, err := ripper.UpdateYtDlp(ctx, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(UpdateResult{Previous: "2026.01.01", Version: "2026.11.01", Updated: true}))
		})

		It("reports the installed version", func() {
			start()
			Expect(ripper.YtDlpVersion(ctx)).To(Equal("2026.01.01"))
		})
	})

	Describe("ScheduleYtDlpUpdates", func() {
		It("updates yt-dlp on the schedule", func() {
			start()
			s := &fakeScheduler{}
			Expect(ripper.ScheduleYtDlpUpdates(ctx, s, "@daily")).To(Succeed())
			Expect(s.crontabs).To(Equal([]string{"@daily"}))

			s.cmds[0]()
			Expect(updater.updates()).To(Equal(1))
		})

		It("schedules nothing when the schedule is empty", func() {
			start()
			s := &fakeScheduler{}
			Expect(ripper.ScheduleYtDlpUpdates(ctx, s, "")).To(Succeed())
			Expect(s.crontabs).To(BeEmpty())
		})
	})
})
