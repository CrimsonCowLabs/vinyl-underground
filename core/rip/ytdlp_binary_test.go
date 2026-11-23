package rip

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("yt-dlp binary", func() {
	Describe("AssetFor", func() {
		DescribeTable("picks the standalone release asset for the platform",
			func(goarch string, musl bool, want string) {
				Expect(AssetFor("linux", goarch, musl)).To(Equal(want))
			},
			Entry("Alpine amd64", "amd64", true, "yt-dlp_musllinux"),
			Entry("Alpine arm64", "arm64", true, "yt-dlp_musllinux_aarch64"),
			Entry("glibc amd64", "amd64", false, "yt-dlp_linux"),
			Entry("glibc arm64", "arm64", false, "yt-dlp_linux_aarch64"),
		)

		It("refuses a platform with no standalone build", func() {
			_, err := AssetFor("linux", "386", true)
			Expect(err).To(HaveOccurred())
			_, err = AssetFor("freebsd", "amd64", false)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Binary.Path", func() {
		const asset = "yt-dlp_musllinux"
		var (
			dir      string
			content  []byte
			sums     string
			requests atomic.Int32
			server   *httptest.Server
			bin      *Binary
		)

		BeforeEach(func() {
			dir = filepath.Join(GinkgoT().TempDir(), "bin")
			content = []byte("#!/bin/sh\necho fake yt-dlp\n")
			sum := sha256.Sum256(content)
			sums = hex.EncodeToString(sum[:]) + "  " + asset + "\n" +
				"0000000000000000000000000000000000000000000000000000000000000000  yt-dlp_linux\n"
			requests.Store(0)
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch r.URL.Path {
				case "/release/SHA2-256SUMS":
					_, _ = w.Write([]byte(sums))
				case "/release/" + asset:
					_, _ = w.Write(content)
				default:
					http.NotFound(w, r)
				}
			}))
			DeferCleanup(server.Close)
			bin = &Binary{Dir: dir, ReleaseURL: server.URL + "/release", Asset: asset, Client: server.Client()}
		})

		It("downloads the release asset on first use and makes it executable", func(ctx SpecContext) {
			path, err := bin.Path(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal(filepath.Join(dir, "yt-dlp")))
			Expect(os.ReadFile(path)).To(Equal(content))
			info, err := os.Stat(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(info.Mode().Perm() & 0o100).ToNot(BeZero())
		})

		It("reuses the binary it already has", func(ctx SpecContext) {
			_, err := bin.Path(ctx)
			Expect(err).ToNot(HaveOccurred())
			before := requests.Load()

			_, err = bin.Path(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(requests.Load()).To(Equal(before))
		})

		It("refuses a download that doesn't match the release's SHA2-256SUMS", func(ctx SpecContext) {
			content = []byte("tampered")
			_, err := bin.Path(ctx)
			Expect(err).To(MatchError(ContainSubstring("checksum")))
			Expect(filepath.Join(dir, "yt-dlp")).ToNot(BeAnExistingFile())
			Expect(os.ReadDir(dir)).To(BeEmpty(), "no partial download is left behind")
		})

		It("refuses an asset the SHA2-256SUMS doesn't list", func(ctx SpecContext) {
			sums = "abc  something_else\n"
			_, err := bin.Path(ctx)
			Expect(err).To(MatchError(ContainSubstring(asset)))
			Expect(filepath.Join(dir, "yt-dlp")).ToNot(BeAnExistingFile())
		})

		It("reports a failed download", func(ctx SpecContext) {
			bin.Asset = "yt-dlp_missing"
			sums += "0000000000000000000000000000000000000000000000000000000000000000  yt-dlp_missing\n"
			_, err := bin.Path(ctx)
			Expect(err).To(MatchError(ContainSubstring("404")))
			Expect(filepath.Join(dir, "yt-dlp")).ToNot(BeAnExistingFile())
		})
	})
})

// versionScript is a stand-in yt-dlp binary that prints version for --version.
func versionScript(version string) []byte {
	return []byte("#!/bin/sh\necho " + version + "\n")
}

// fakeReleases serves yt-dlp releases the way GitHub does: /releases/latest/download/<name>
// redirects to /releases/download/<tag>/<name>, where the release's assets are.
type fakeReleases struct {
	mu       sync.Mutex
	latest   string
	assets   map[string]map[string][]byte // tag → asset name → content
	requests []string
	// afterSums runs once a release's SHA2-256SUMS has been served, e.g. to publish a new release.
	afterSums func()
	// down makes looking up the latest release fail, as when GitHub is down or limits the lookups.
	down bool
	// stall, when set, holds the next lookup of the latest release until it is closed.
	stall  chan struct{}
	server *httptest.Server
}

func newFakeReleases(asset string, tags ...string) *fakeReleases {
	f := &fakeReleases{assets: map[string]map[string][]byte{}}
	for _, tag := range tags {
		f.publish(tag, asset, versionScript(tag))
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	DeferCleanup(f.server.Close)
	return f
}

// publish adds a release with one asset and its SHA2-256SUMS, and makes it the latest.
func (f *fakeReleases) publish(tag, asset string, content []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := sha256.Sum256(content)
	f.assets[tag] = map[string][]byte{
		asset:          content,
		"SHA2-256SUMS": []byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n"),
	}
	f.latest = tag
}

func (f *fakeReleases) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.URL.Path)
	latest, down := f.latest, f.down
	var stall chan struct{}
	if strings.HasPrefix(r.URL.Path, "/releases/latest/") {
		stall, f.stall = f.stall, nil
	}
	f.mu.Unlock()
	if name, ok := strings.CutPrefix(r.URL.Path, "/releases/latest/download/"); ok {
		if stall != nil {
			<-stall
		}
		if down {
			http.Error(w, "rate limit exceeded", http.StatusForbidden)
			return
		}
		http.Redirect(w, r, "/releases/download/"+latest+"/"+name, http.StatusFound)
		return
	}
	tag, name, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/releases/download/"), "/")
	f.mu.Lock()
	content, ok := f.assets[tag][name]
	after := f.afterSums
	f.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(content)
	if name == "SHA2-256SUMS" && after != nil {
		after()
	}
}

// setDown makes looking up the latest release fail (down) or work again.
func (f *fakeReleases) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

// stallNext holds the next lookup of the latest release until the returned function is called.
// Cleanup calls it too, so a test that fails part-way doesn't leave the server waiting.
func (f *fakeReleases) stallNext() (release func()) {
	stall := make(chan struct{})
	f.mu.Lock()
	f.stall = stall
	f.mu.Unlock()
	release = sync.OnceFunc(func() { close(stall) })
	DeferCleanup(release)
	return release
}

// lookups counts the requests for the latest release.
func (f *fakeReleases) lookups() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.requests {
		if strings.HasPrefix(p, "/releases/latest/") {
			n++
		}
	}
	return n
}

// downloads counts the requests for a release asset (from any release).
func (f *fakeReleases) downloads(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.requests {
		if strings.HasPrefix(p, "/releases/download/") && strings.HasSuffix(p, "/"+name) {
			n++
		}
	}
	return n
}

var _ = Describe("Binary.Update", func() {
	const asset = "yt-dlp_musllinux"
	var (
		dir      string
		path     string
		releases *fakeReleases
		bin      *Binary
	)

	BeforeEach(func() {
		dir = filepath.Join(GinkgoT().TempDir(), "bin")
		path = filepath.Join(dir, "yt-dlp")
		releases = newFakeReleases(asset, "2026.10.01", "2026.11.01")
		bin = &Binary{
			Dir: dir, ReleaseURL: releases.server.URL + "/releases/latest/download", Asset: asset,
			Client: releases.server.Client(), TempDir: filepath.Join(dir, "tmp-update"),
		}
	})

	plant := func(version string) {
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		Expect(os.WriteFile(path, versionScript(version), 0o755)).To(Succeed())
	}

	It("installs the latest release when there is no binary yet", func(ctx SpecContext) {
		res, err := bin.Update(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(Equal(UpdateResult{Previous: "", Version: "2026.11.01", Updated: true}))
		Expect(bin.Version(ctx)).To(Equal("2026.11.01"))
	})

	It("replaces an older binary with the latest release", func(ctx SpecContext) {
		plant("2026.01.01")
		res, err := bin.Update(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(Equal(UpdateResult{Previous: "2026.01.01", Version: "2026.11.01", Updated: true}))
		Expect(os.ReadFile(path)).To(Equal(versionScript("2026.11.01")))
		Expect(bin.Version(ctx)).To(Equal("2026.11.01"))
	})

	It("downloads nothing when the installed version is the latest", func(ctx SpecContext) {
		plant("2026.11.01")
		res, err := bin.Update(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(Equal(UpdateResult{Previous: "2026.11.01", Version: "2026.11.01", Updated: false}))
		Expect(releases.downloads(asset)).To(BeZero())
		Expect(releases.downloads("SHA2-256SUMS")).To(BeZero(), "only the latest tag is looked up")
	})

	It("fetches the binary from the same release as its checksums, even if a new release lands in between", func(ctx SpecContext) {
		releases.afterSums = func() {
			releases.mu.Lock()
			releases.afterSums = nil
			releases.mu.Unlock()
			releases.publish("2026.12.01", asset, versionScript("2026.12.01"))
		}
		res, err := bin.Update(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Version).To(Equal("2026.11.01"))
		Expect(os.ReadFile(path)).To(Equal(versionScript("2026.11.01")))
	})

	It("installs from a release folder that doesn't redirect, such as a mirror", func(ctx SpecContext) {
		bin.ReleaseURL = releases.server.URL + "/releases/download/2026.10.01"
		res, err := bin.Update(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Version).To(Equal("2026.10.01"))

		By("not downloading it again when it is already installed")
		res, err = bin.Update(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Updated).To(BeFalse())
		Expect(releases.downloads(asset)).To(Equal(1))
	})

	It("downloads nothing when the release's tag isn't its version but its file is already here", func(ctx SpecContext) {
		releases.publish("stable", asset, versionScript("2026.11.01"))
		plant("2026.11.01")
		res, err := bin.Update(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Updated).To(BeFalse())
		Expect(releases.downloads(asset)).To(BeZero())
	})

	Describe("a failed update", func() {
		BeforeEach(func() { plant("2026.01.01") })

		keepsTheOldBinary := func(ctx context.Context) {
			Expect(os.ReadFile(path)).To(Equal(versionScript("2026.01.01")))
			Expect(bin.Version(ctx)).To(Equal("2026.01.01"))
			entries, err := os.ReadDir(dir)
			Expect(err).ToNot(HaveOccurred())
			for _, e := range entries {
				Expect(e.Name()).ToNot(HaveSuffix(".part"), "no partial download is left behind")
			}
		}

		It("keeps the previous binary when the download fails its checksum", func(ctx SpecContext) {
			releases.mu.Lock()
			releases.assets["2026.11.01"][asset] = []byte("tampered")
			releases.mu.Unlock()
			_, err := bin.Update(ctx)
			Expect(err).To(MatchError(ContainSubstring("checksum")))
			keepsTheOldBinary(ctx)
		})

		It("keeps the previous binary when the download fails", func(ctx SpecContext) {
			releases.mu.Lock()
			delete(releases.assets["2026.11.01"], asset)
			releases.mu.Unlock()
			_, err := bin.Update(ctx)
			Expect(err).To(MatchError(ContainSubstring("404")))
			keepsTheOldBinary(ctx)
		})

		It("keeps the previous binary when the new one doesn't run", func(ctx SpecContext) {
			releases.publish("2026.11.02", asset, []byte("#!/bin/sh\nexit 1\n"))
			_, err := bin.Update(ctx)
			Expect(err).To(MatchError(ContainSubstring("doesn't run")))
			keepsTheOldBinary(ctx)
		})

		It("keeps the previous binary when the release server is down", func(ctx SpecContext) {
			releases.server.Close()
			_, err := bin.Update(ctx)
			Expect(err).To(HaveOccurred())
			keepsTheOldBinary(ctx)
		})
	})

	It("reports no version before yt-dlp is installed", func(ctx SpecContext) {
		Expect(bin.Version(ctx)).To(BeEmpty())
	})
})

var _ = Describe("Binary.Latest", func() {
	const asset = "yt-dlp_musllinux"
	var (
		dir      string
		releases *fakeReleases
		bin      *Binary
		now      time.Time
	)

	BeforeEach(func() {
		dir = filepath.Join(GinkgoT().TempDir(), "bin")
		releases = newFakeReleases(asset, "2026.10.01", "2026.11.01")
		now = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
		bin = &Binary{
			Dir: dir, ReleaseURL: releases.server.URL + "/releases/latest/download", Asset: asset,
			Client: releases.server.Client(), TempDir: filepath.Join(dir, "tmp-update"),
			now: func() time.Time { return now },
		}
	})

	It("returns the latest release's version", func(ctx SpecContext) {
		Expect(bin.Latest(ctx)).To(Equal("2026.11.01"))
		Expect(releases.downloads(asset)).To(BeZero())
		Expect(releases.downloads("SHA2-256SUMS")).To(BeZero(), "only the latest tag is looked up")
	})

	It("looks it up at most once an hour", func(ctx SpecContext) {
		Expect(bin.Latest(ctx)).To(Equal("2026.11.01"))
		releases.publish("2026.12.01", asset, versionScript("2026.12.01"))
		now = now.Add(59 * time.Minute)
		Expect(bin.Latest(ctx)).To(Equal("2026.11.01"))
		Expect(releases.lookups()).To(Equal(1))

		By("looking it up again once the hour is up")
		now = now.Add(2 * time.Minute)
		Expect(bin.Latest(ctx)).To(Equal("2026.12.01"))
		Expect(releases.lookups()).To(Equal(2))
	})

	It("reports a failed lookup, and doesn't try again straight away", func(ctx SpecContext) {
		releases.setDown(true)
		tag, err := bin.Latest(ctx)
		Expect(err).To(HaveOccurred())
		Expect(tag).To(BeEmpty(), "a failed lookup returns no version")
		tag, err = bin.Latest(ctx)
		Expect(err).To(HaveOccurred())
		Expect(tag).To(BeEmpty())
		Expect(releases.lookups()).To(Equal(1))

		By("trying again a few minutes later")
		releases.setDown(false)
		releases.publish("2026.12.01", asset, versionScript("2026.12.01"))
		now = now.Add(failedLatestKeptFor + time.Second)
		Expect(bin.Latest(ctx)).To(Equal("2026.12.01"))
	})

	It("knows the latest release after an update, without looking it up again", func(ctx SpecContext) {
		Expect(bin.Latest(ctx)).To(Equal("2026.11.01"))
		releases.publish("2026.12.01", asset, versionScript("2026.12.01"))
		_, err := bin.Update(ctx)
		Expect(err).ToNot(HaveOccurred())
		lookups := releases.lookups()

		Expect(bin.Latest(ctx)).To(Equal("2026.12.01"))
		Expect(releases.lookups()).To(Equal(lookups))
	})

	It("returns no version for a release folder that doesn't redirect, such as a mirror", func(ctx SpecContext) {
		bin.ReleaseURL = releases.server.URL + "/releases/download/2026.10.01"
		Expect(bin.Latest(ctx)).To(BeEmpty())
	})

	It("doesn't remember the lookup as failed when the page waiting on it is closed", func(ctx SpecContext) {
		release := releases.stallNext()
		pageCtx, closePage := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			_, err := bin.Latest(pageCtx)
			done <- err
		}()
		Eventually(releases.lookups).Should(Equal(1))
		closePage()
		Eventually(done).Should(Receive(MatchError(context.Canceled)))

		release()
		Expect(bin.Latest(ctx)).To(Equal("2026.11.01"))
		Expect(releases.lookups()).To(Equal(1), "the lookup the closed page started is kept")
	})

	It("looks up nothing on a platform with no standalone build", func(ctx SpecContext) {
		bin.Asset = ""
		Expect(bin.Latest(ctx)).To(BeEmpty(), "Update can't install a release, so none is offered")
		Expect(releases.lookups()).To(BeZero())
	})

	It("doesn't hold up an update while a lookup waits on GitHub", func(ctx SpecContext) {
		release := releases.stallNext()
		go func() { _, _ = bin.Latest(ctx) }()
		Eventually(releases.lookups).Should(Equal(1))

		updated := make(chan error, 1)
		go func() {
			_, err := bin.Update(ctx)
			updated <- err
		}()
		Eventually(updated, "5s").Should(Receive(BeNil()))
		release()
	})

	It("shares one lookup among pages loaded at the same time", func(ctx SpecContext) {
		release := releases.stallNext()
		type result struct {
			tag string
			err error
		}
		results := make(chan result, 2)
		for range 2 {
			go func() {
				tag, err := bin.Latest(ctx)
				results <- result{tag, err}
			}()
		}
		Eventually(releases.lookups).Should(Equal(1))
		Consistently(releases.lookups, "200ms").Should(Equal(1))
		release()
		for range 2 {
			Eventually(results).Should(Receive(Equal(result{tag: "2026.11.01"})))
		}
		Expect(releases.lookups()).To(Equal(1))
	})
})

var _ = Describe("NewerVersion", func() {
	DescribeTable("tells whether the latest release is newer than the installed version",
		func(latest, installed string, want bool) {
			Expect(NewerVersion(latest, installed)).To(Equal(want))
		},
		Entry("a later day", "2026.11.01", "2026.10.01", true),
		Entry("the same version", "2026.11.01", "2026.11.01", false),
		Entry("an older release", "2026.10.01", "2026.11.01", false),
		Entry("numbers, not text", "2026.10.1", "2026.9.30", true),
		Entry("a second release on the same day", "2026.11.01.1", "2026.11.01", true),
		Entry("a nightly newer than the latest stable", "2026.11.01", "2026.11.01.232857", false),
		Entry("a nightly older than the latest stable", "2026.11.02", "2026.11.01.232857", true),
		Entry("nothing installed yet", "2026.11.01", "", true),
		Entry("no latest release known", "", "2026.11.01", false),
		Entry("an unreadable version that differs", "2026.11.01", "master", true),
		Entry("an unreadable version that matches", "master", "master", false),
	)
})
