package stations_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core/stations"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeWriter stands in for the LLM writing Blurbs: each Blurb names the DJ and both Tracks.
// Until release is closed (when set), it hangs like a slow LLM.
type fakeWriter struct {
	mu      sync.Mutex
	written [][2]string // the Track change of each Blurb asked for: {previous, next}
	release chan struct{}
	err     error
}

func (f *fakeWriter) Write(_ context.Context, dj stations.DJ, _ string, prev, next model.MediaFile) (string, error) {
	f.mu.Lock()
	f.written = append(f.written, [2]string{prev.ID, next.ID})
	release := f.release
	f.mu.Unlock()
	if release != nil {
		<-release
	}
	if f.err != nil {
		return "", f.err
	}
	return fmt.Sprintf("%s: that was %s, here's %s", dj.Name, prev.Title, next.Title), nil
}

func (f *fakeWriter) asked() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]string(nil), f.written...)
}

// changes lists the Track changes between tracks, in order.
func changes(tracks model.MediaFiles) [][2]string {
	var res [][2]string
	for i := 1; i < len(tracks); i++ {
		res = append(res, [2]string{tracks[i-1].ID, tracks[i].ID})
	}
	return res
}

var _ = Describe("Blurbs", Ordered, func() {
	var ds model.DataStore
	var writer *fakeWriter
	var svc stations.Service
	var ctx context.Context
	var seq int

	BeforeAll(func() {
		ds = persistence.New(db.Db())
		dave := model.User{ID: "dave-id", UserName: "dave", Name: "dave", NewPassword: "pw"}
		Expect(ds.User(context.Background()).Put(&dave)).To(Succeed())
		Expect(ds.User(context.Background()).SetUserLibraries(dave.ID, []int{1})).To(Succeed())
		dave.Libraries = model.Libraries{{ID: 1}}
		ctx = request.WithUser(context.Background(), dave)
	})

	BeforeEach(func() {
		for _, table := range []string{"station_track", "annotation", "media_file"} {
			_, err := db.Db().Exec("delete from " + table)
			Expect(err).ToNot(HaveOccurred())
		}
		for range 30 {
			seq++
			mf := model.MediaFile{
				ID: fmt.Sprintf("blurb-%04d", seq), LibraryID: 1, Path: fmt.Sprintf("b/%d.mp3", seq),
				Title: fmt.Sprintf("Tune %d", seq), Artist: fmt.Sprintf("Band %d", seq),
				AlbumArtist: "Various", Album: "Album", Tags: model.Tags{},
			}
			Expect(ds.MediaFile(ctx).Put(&mf)).To(Succeed())
		}
		writer = &fakeWriter{}
		svc = stations.New(ds, nil, writer)
	})

	next := func(djID string, count int) model.MediaFiles {
		tracks, err := svc.Next(ctx, djID, "", count)
		Expect(err).ToNot(HaveOccurred())
		return tracks
	}

	It("writes a Blurb ahead for each Track change of the Tracks served, in the DJ's voice", func() {
		tracks := next("night-owl", 5)
		stations.WaitForBlurbs(svc)

		Expect(writer.asked()).To(Equal(changes(tracks)))
		blurb, ok := svc.Blurb(ctx, "night-owl", tracks[1].ID, tracks[0].ID)
		Expect(ok).To(BeTrue())
		Expect(blurb.DJName).To(Equal("Night Owl"))
		Expect(blurb.Text).To(Equal(fmt.Sprintf("Night Owl: that was %s, here's %s", tracks[0].Title, tracks[1].Title)))
	})

	It("never waits on the writer: the Tracks are served at once, and a Blurb not written yet isn't there", func() {
		writer.release = make(chan struct{})
		DeferCleanup(func() { stations.WaitForBlurbs(svc) })
		defer close(writer.release)

		done := make(chan model.MediaFiles)
		go func() {
			defer GinkgoRecover()
			done <- next("sunny", 5)
		}()
		var tracks model.MediaFiles
		Eventually(done, time.Second).Should(Receive(&tracks))
		Eventually(writer.asked).ShouldNot(BeEmpty())

		_, ok := svc.Blurb(ctx, "sunny", tracks[1].ID, tracks[0].ID)
		Expect(ok).To(BeFalse())
	})

	It("follows on from the last refill, so the Track change between refills gets a Blurb too", func() {
		first := next("night-owl", 3)
		second := next("night-owl", 3)
		stations.WaitForBlurbs(svc)

		_, ok := svc.Blurb(ctx, "night-owl", second[0].ID, first[2].ID)
		Expect(ok).To(BeTrue())
		Expect(writer.asked()).To(Equal(changes(append(first, second...))))
	})

	It("has no Blurb for a Track change other than the one written", func() {
		tracks := next("night-owl", 5)
		stations.WaitForBlurbs(svc)

		_, ok := svc.Blurb(ctx, "night-owl", tracks[3].ID, tracks[1].ID) // the Listener skipped back
		Expect(ok).To(BeFalse())
		_, ok = svc.Blurb(ctx, "sunny", tracks[1].ID, tracks[0].ID)
		Expect(ok).To(BeFalse())
	})

	It("plays without a Blurb when writing it fails", func() {
		writer.err = errors.New("AI unavailable")
		tracks := next("night-owl", 5)
		stations.WaitForBlurbs(svc)

		Expect(tracks).To(HaveLen(5))
		_, ok := svc.Blurb(ctx, "night-owl", tracks[1].ID, tracks[0].ID)
		Expect(ok).To(BeFalse())
	})

	It("starts afresh on a new Station, never linking it to the old one", func() {
		owl := next("night-owl", 3)
		stations.WaitForBlurbs(svc)
		sunny := next("sunny", 3)
		stations.WaitForBlurbs(svc)

		Expect(writer.asked()).To(Equal(append(changes(owl), changes(sunny)...)))
		_, ok := svc.Blurb(ctx, "night-owl", owl[1].ID, owl[0].ID)
		Expect(ok).To(BeFalse())
	})

	It("stops writing the old Station's Blurbs once the Listener starts another", func() {
		writer.release = make(chan struct{})
		owl := next("night-owl", 5)
		Eventually(writer.asked).Should(HaveLen(1)) // writing the first, slowly

		sunny := next("sunny", 3)
		Consistently(writer.asked, 100*time.Millisecond).Should(HaveLen(1)) // one at a time
		close(writer.release)
		stations.WaitForBlurbs(svc)

		Expect(writer.asked()).To(Equal(append([][2]string{{owl[0].ID, owl[1].ID}}, changes(sunny)...)))
		_, ok := svc.Blurb(ctx, "sunny", sunny[1].ID, sunny[0].ID)
		Expect(ok).To(BeTrue())
	})

	It("doesn't write Blurbs for Track changes the Listener has already passed", func() {
		writer.release = make(chan struct{})
		tracks := next("night-owl", 5)
		Eventually(writer.asked).Should(HaveLen(1)) // writing the first, slowly

		// The Listener skipped ahead to the fourth Track meanwhile.
		_, ok := svc.Blurb(ctx, "night-owl", tracks[3].ID, tracks[2].ID)
		Expect(ok).To(BeFalse())
		close(writer.release)
		stations.WaitForBlurbs(svc)

		Expect(writer.asked()).To(Equal([][2]string{
			{tracks[0].ID, tracks[1].ID},
			{tracks[3].ID, tracks[4].ID},
		}))
	})

	It("has no Blurbs without a writer (no LLM), and the Station still plays", func() {
		noLLM := stations.New(ds, nil, nil)
		tracks, err := noLLM.Next(ctx, "night-owl", "", 5)
		Expect(err).ToNot(HaveOccurred())
		Expect(tracks).To(HaveLen(5))
		_, ok := noLLM.Blurb(ctx, "night-owl", tracks[1].ID, tracks[0].ID)
		Expect(ok).To(BeFalse())
	})
})
