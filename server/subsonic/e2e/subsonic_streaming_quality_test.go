package e2e

import (
	"net/http"
	"net/http/httptest"

	"github.com/navidrome/navidrome/core/streamquality"
	"github.com/navidrome/navidrome/server/subsonic/responses"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	phoneUserAgent   = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36"
	desktopUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0"
	iPhoneUserAgent  = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1"

	// safariProfile is what browserProfile.js sends from Safari, which only transcodes to MP3.
	safariProfile = `{
		"name": "NavidromeUI",
		"directPlayProfiles": [
			{"containers": ["mp3"], "audioCodecs": ["mp3"], "protocols": ["http"]},
			{"containers": ["flac"], "audioCodecs": ["flac"], "protocols": ["http"]},
			{"containers": ["mp4"], "audioCodecs": ["aac"], "protocols": ["http"]}
		],
		"transcodingProfiles": [
			{"container": "mp3", "audioCodec": "mp3", "protocol": "http"}
		]
	}`

	// webAppProfile is what the web app's browserProfile.js sends from Chrome.
	webAppProfile = `{
		"name": "NavidromeUI",
		"directPlayProfiles": [
			{"containers": ["mp3"], "audioCodecs": ["mp3"], "protocols": ["http"]},
			{"containers": ["ogg"], "audioCodecs": ["opus"], "protocols": ["http"]},
			{"containers": ["flac"], "audioCodecs": ["flac"], "protocols": ["http"]}
		],
		"transcodingProfiles": [
			{"container": "flac", "audioCodec": "flac", "protocol": "http"},
			{"container": "ogg", "audioCodec": "opus", "protocol": "http"},
			{"container": "mp3", "audioCodec": "mp3", "protocol": "http"}
		]
	}`
)

var _ = Describe("Streaming quality", Ordered, func() {
	var flacTrackID string

	BeforeAll(func() {
		setupTestDB()
		songs, err := ds.MediaFile(ctx).GetAll()
		Expect(err).ToNot(HaveOccurred())
		for _, s := range songs {
			if s.Title == "TC FLAC Standard" {
				flacTrackID = s.ID
			}
		}
		Expect(flacTrackID).ToNot(BeEmpty())
	})

	AfterEach(func() {
		Expect(streamquality.New(ds).SetPreference(ctx, adminUser.ID, streamquality.Auto)).To(Succeed())
	})

	// webAppRequest builds a request as the web app sends it, from the given device.
	webAppRequest := func(r *http.Request, userAgent string) *http.Request {
		q := r.URL.Query()
		q.Set("c", streamquality.WebAppClient)
		r.URL.RawQuery = q.Encode()
		r.Header.Set("User-Agent", userAgent)
		return r
	}

	decideWith := func(profile, userAgent string) *responses.TranscodeDecision {
		r := buildPostReq(adminUser, "getTranscodeDecision", profile, "mediaId", flacTrackID, "mediaType", "song")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, webAppRequest(r, userAgent))
		resp := parseJSONResponse(w)
		Expect(resp.Status).To(Equal(responses.StatusOK))
		Expect(resp.TranscodeDecision).ToNot(BeNil())
		return resp.TranscodeDecision
	}
	decide := func(userAgent string) *responses.TranscodeDecision {
		return decideWith(webAppProfile, userAgent)
	}

	It("gives a phone Opus at 160 kbps with no setup", func() {
		decision := decide(phoneUserAgent)
		Expect(decision.CanDirectPlay).To(BeFalse())
		Expect(decision.CanTranscode).To(BeTrue())
		Expect(decision.TranscodeStream.Codec).To(Equal("opus"))
		Expect(decision.TranscodeStream.AudioBitrate).To(Equal(int32(160_000)))
	})

	It("gives an iPhone 160 kbps MP3, because Safari cannot stream transcoded Ogg Opus", func() {
		decision := decideWith(safariProfile, iPhoneUserAgent)
		Expect(decision.CanDirectPlay).To(BeFalse())
		Expect(decision.CanTranscode).To(BeTrue())
		Expect(decision.TranscodeStream.Codec).To(Equal("mp3"))
		Expect(decision.TranscodeStream.AudioBitrate).To(Equal(int32(160_000)))
	})

	It("gives a desktop the original File", func() {
		decision := decide(desktopUserAgent)
		Expect(decision.CanDirectPlay).To(BeTrue())
	})

	It("gives a phone the original File when the Listener chose Original", func() {
		Expect(streamquality.New(ds).SetPreference(ctx, adminUser.ID, streamquality.Original)).To(Succeed())
		decision := decide(phoneUserAgent)
		Expect(decision.CanDirectPlay).To(BeTrue())
	})

	It("gives a desktop Opus at 160 kbps when the Listener chose Data saver", func() {
		Expect(streamquality.New(ds).SetPreference(ctx, adminUser.ID, streamquality.DataSaver)).To(Succeed())
		decision := decide(desktopUserAgent)
		Expect(decision.CanTranscode).To(BeTrue())
		Expect(decision.TranscodeStream.Codec).To(Equal("opus"))
		Expect(decision.TranscodeStream.AudioBitrate).To(Equal(int32(160_000)))
	})

	It("streams Opus at 160 kbps from the plain stream endpoint on a phone", func() {
		r := buildReq(adminUser, "stream", "id", flacTrackID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, webAppRequest(r, phoneUserAgent))
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(streamerSpy.LastRequest.Format).To(Equal("opus"))
		Expect(streamerSpy.LastRequest.BitRate).To(Equal(160))
	})
})
