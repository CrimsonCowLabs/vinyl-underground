package streamquality_test

import (
	"context"

	"github.com/navidrome/navidrome/core/streamquality"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	androidChrome = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36"
	iPhoneSafari  = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1"
	linuxFirefox  = "Mozilla/5.0 (X11; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0"
	macSafari     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15"
	windowsChrome = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	androidNative = "Dalvik/2.1.0 (Linux; U; Android 14; Pixel 8 Build/AP2A.240805.005)"
)

type noTranscodings struct{ model.TranscodingRepository }

func (noTranscodings) FindByFormat(string) (*model.Transcoding, error) { return nil, model.ErrNotFound }

var _ = Describe("Service", func() {
	var (
		ctx context.Context
		ds  *tests.MockDataStore
		svc streamquality.Service
	)

	BeforeEach(func() {
		ctx = context.Background()
		ds = &tests.MockDataStore{MockedTranscoding: &tests.MockTranscodingRepo{}}
		svc = streamquality.New(ds)
	})

	Describe("Transcoding", func() {
		It("streams Opus at 160 kbps to the web app on a phone", func() {
			trc, err := svc.Transcoding(ctx, "listener1", streamquality.WebAppClient, androidChrome)
			Expect(err).ToNot(HaveOccurred())
			Expect(trc).ToNot(BeNil())
			Expect(trc.TargetFormat).To(Equal("opus"))
			Expect(trc.DefaultBitRate).To(Equal(160))
		})

		It("streams Opus at 160 kbps to the web app on an iPhone", func() {
			trc, err := svc.Transcoding(ctx, "listener1", streamquality.WebAppClient, iPhoneSafari)
			Expect(err).ToNot(HaveOccurred())
			Expect(trc).ToNot(BeNil())
			Expect(trc.TargetFormat).To(Equal("opus"))
		})

		DescribeTable("streams the original File to the web app on a desktop",
			func(userAgent string) {
				trc, err := svc.Transcoding(ctx, "listener1", streamquality.WebAppClient, userAgent)
				Expect(err).ToNot(HaveOccurred())
				Expect(trc).To(BeNil())
			},
			Entry("Firefox on Linux", linuxFirefox),
			Entry("Safari on macOS", macSafari),
			Entry("Chrome on Windows", windowsChrome),
			Entry("an unknown User-Agent", ""),
		)

		It("falls back to the original File when no Opus transcoding is configured", func() {
			ds.MockedTranscoding = &noTranscodings{}
			trc, err := svc.Transcoding(ctx, "listener1", streamquality.WebAppClient, androidChrome)
			Expect(err).ToNot(HaveOccurred())
			Expect(trc).To(BeNil())
		})

		It("leaves native Subsonic clients on a phone to negotiate their own quality", func() {
			trc, err := svc.Transcoding(ctx, "listener1", "Symfonium", androidNative)
			Expect(err).ToNot(HaveOccurred())
			Expect(trc).To(BeNil())
		})
	})

	Describe("Listener preference", func() {
		It("defaults to automatic", func() {
			pref, err := svc.Preference(ctx, "listener1")
			Expect(err).ToNot(HaveOccurred())
			Expect(pref).To(Equal(streamquality.Auto))
		})

		It("remembers what the Listener chose", func() {
			Expect(svc.SetPreference(ctx, "listener1", streamquality.Original)).To(Succeed())
			pref, err := svc.Preference(ctx, "listener1")
			Expect(err).ToNot(HaveOccurred())
			Expect(pref).To(Equal(streamquality.Original))
		})

		It("is private to each Listener", func() {
			Expect(svc.SetPreference(ctx, "listener1", streamquality.DataSaver)).To(Succeed())
			pref, err := svc.Preference(ctx, "listener2")
			Expect(err).ToNot(HaveOccurred())
			Expect(pref).To(Equal(streamquality.Auto))
		})

		It("rejects an unknown preference", func() {
			err := svc.SetPreference(ctx, "listener1", streamquality.Preference("lossless-ish"))
			Expect(err).To(MatchError(streamquality.ErrInvalidPreference))
		})

		It("streams the original File on a phone when the Listener chose Original", func() {
			Expect(svc.SetPreference(ctx, "listener1", streamquality.Original)).To(Succeed())
			trc, err := svc.Transcoding(ctx, "listener1", streamquality.WebAppClient, androidChrome)
			Expect(err).ToNot(HaveOccurred())
			Expect(trc).To(BeNil())
		})

		It("streams Opus at 160 kbps on a desktop when the Listener chose Data saver", func() {
			Expect(svc.SetPreference(ctx, "listener1", streamquality.DataSaver)).To(Succeed())
			trc, err := svc.Transcoding(ctx, "listener1", streamquality.WebAppClient, linuxFirefox)
			Expect(err).ToNot(HaveOccurred())
			Expect(trc).ToNot(BeNil())
			Expect(trc.TargetFormat).To(Equal("opus"))
			Expect(trc.DefaultBitRate).To(Equal(160))
		})

		It("does not apply to native Subsonic clients, even with Data saver", func() {
			Expect(svc.SetPreference(ctx, "listener1", streamquality.DataSaver)).To(Succeed())
			trc, err := svc.Transcoding(ctx, "listener1", "Symfonium", androidNative)
			Expect(err).ToNot(HaveOccurred())
			Expect(trc).To(BeNil())
		})
	})
})
