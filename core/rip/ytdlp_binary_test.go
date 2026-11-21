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
	server    *httptest.Server
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
	latest := f.latest
	f.mu.Unlock()
	if name, ok := strings.CutPrefix(r.URL.Path, "/releases/latest/download/"); ok {
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
