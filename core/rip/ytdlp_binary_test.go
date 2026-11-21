package rip

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
