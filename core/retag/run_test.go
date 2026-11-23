package retag

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeLibrary holds Tracks, and writes edits into them as the editor and a rescan would.
type fakeLibrary struct {
	mfs      model.MediaFiles
	edits    map[string]tagedit.Values
	opts     []tagedit.EditOptions
	rescans  []string
	failWith map[string]error
}

func (l *fakeLibrary) EditWith(_ context.Context, id string, v tagedit.Values, opts tagedit.EditOptions) (*tagedit.Result, error) {
	if err := l.failWith[id]; err != nil {
		return nil, err
	}
	l.edits[id] = v
	l.opts = append(l.opts, opts)
	for i := range l.mfs {
		mf := &l.mfs[i]
		if mf.ID != id {
			continue
		}
		for k, vs := range v {
			switch k {
			case "title":
				mf.Title = vs[0]
			case "artist":
				mf.Artist = vs[0]
			case "album":
				mf.Album = vs[0]
			case "albumartist":
				mf.AlbumArtist = vs[0]
			case "tracknumber":
				mf.TrackNumber, _ = strconv.Atoi(vs[0])
			case "discnumber":
				mf.DiscNumber, _ = strconv.Atoi(vs[0])
			case "date":
				mf.Year, _ = strconv.Atoi(vs[0])
			case "musicbrainz_trackid":
				mf.MbzRecordingID = vs[0]
			}
		}
	}
	return &tagedit.Result{Changed: true}, nil
}

func (l *fakeLibrary) Rescan(_ context.Context, _ int, dir string) error {
	l.rescans = append(l.rescans, dir)
	return nil
}

func (l *fakeLibrary) lib() Library {
	return Library{ID: 1, Files: func(context.Context) (model.MediaFiles, error) { return l.mfs, nil }}
}

type fakeMB struct {
	searches []string
	releases map[string]*musicbrainz.Release
	found    []musicbrainz.Release
}

func (m *fakeMB) SearchReleases(_ context.Context, artist, title string) ([]musicbrainz.Release, error) {
	m.searches = append(m.searches, artist+"|"+title)
	return m.found, nil
}

func (m *fakeMB) Release(_ context.Context, id string) (*musicbrainz.Release, error) {
	return m.releases[id], nil
}

func track(id, p, title string, n int, duration float32) model.MediaFile {
	return model.MediaFile{ID: id, LibraryID: 1, Path: p, Title: title, Artist: "Radiohead", AlbumArtist: "Radiohead",
		Album: "OK Computer", TrackNumber: n, Year: 1997, Duration: duration}
}

var _ = Describe("Run", func() {
	var lib *fakeLibrary
	var report *bytes.Buffer
	var opts Options

	BeforeEach(func() {
		lib = &fakeLibrary{edits: map[string]tagedit.Values{}, failWith: map[string]error{}}
		report = &bytes.Buffer{}
		opts = Options{Writer: lib, Apply: true, JobID: "job-1", Report: report}
	})

	run := func() Summary {
		sum, err := Run(context.Background(), []Library{lib.lib()}, opts)
		Expect(err).ToNot(HaveOccurred())
		return sum
	}

	It("makes the local fixes, as one job, rescanning each changed folder once", func() {
		lib.mfs = model.MediaFiles{
			track("1", "Radiohead/OK Computer/01 - Airbag.mp3", "01 - Airbag", 0, 200),
			track("2", "Radiohead/OK Computer/02 - Paranoid Android.mp3", "02 - Paranoid Android", 2, 201),
			track("3", "Radiohead/OK Computer/03 Subterranean.mp3", "Subterranean", 3, 202),
		}
		sum := run()
		Expect(lib.edits).To(HaveKeyWithValue("1", tagedit.Values{"title": {"Airbag"}, "tracknumber": {"1"}}))
		Expect(lib.edits).To(HaveKeyWithValue("2", tagedit.Values{"title": {"Paranoid Android"}}))
		Expect(lib.edits).NotTo(HaveKey("3"))
		Expect(lib.opts).To(HaveEach(tagedit.EditOptions{JobID: "job-1", NoRescan: true}))
		Expect(lib.rescans).To(Equal([]string{"Radiohead/OK Computer"}))
		Expect(sum.Changed).To(Equal(2))
		Expect(sum.Unchanged).To(Equal(1))
		Expect(report.String()).To(ContainSubstring("changed\tRadiohead/OK Computer/01 - Airbag.mp3\t"))
	})

	It("writes nothing without Apply, and reports what it would change", func() {
		opts.Apply = false
		lib.mfs = model.MediaFiles{track("1", "A/B/01 - Airbag.mp3", "01 - Airbag", 1, 200)}
		sum := run()
		Expect(lib.edits).To(BeEmpty())
		Expect(lib.rescans).To(BeEmpty())
		Expect(sum.Changed).To(Equal(1))
		Expect(report.String()).To(ContainSubstring(`would-change	A/B/01 - Airbag.mp3	title: "01 - Airbag" → "Airbag"`))
	})

	It("leaves out Quarantine, Duplicates, other formats and Files outside the path", func() {
		lib.mfs = model.MediaFiles{
			track("q", "_Quarantine/A/01 - X.mp3", "01 - X", 0, 200),
			track("d", "A/B/Song 1.mp3", "01 - Song", 0, 200),
			track("o", "A/B/Song.mp3", "Song", 1, 200),
			track("f", "A/B/02 - Y.flac", "02 - Y", 0, 200),
			track("p", "C/01 - Z.mp3", "01 - Z", 0, 200),
		}
		opts.PathPrefix = "A/"
		sum := run()
		Expect(lib.edits).To(BeEmpty())
		Expect(sum.Skipped).To(Equal(1))
		Expect(report.String()).To(ContainSubstring("skipped\tA/B/Song 1.mp3"))
	})

	It("never retags a File in Quarantine", func() {
		lib.mfs = model.MediaFiles{
			track("q", "_Quarantine/A/01 - X.mp3", "01 - X", 0, 200),
			track("k", "A/01 - Y.mp3", "01 - Y", 0, 200),
		}
		sum := run()
		Expect(lib.edits).NotTo(HaveKey("q"))
		Expect(lib.edits).To(HaveKey("k"))
		Expect(sum.Files).To(Equal(1))
	})

	It("takes a path as a folder, not a prefix of a name", func() {
		lib.mfs = model.MediaFiles{
			track("1", "Radiohead/01 - X.mp3", "01 - X", 0, 200),
			track("2", "Radiohead Tribute/01 - Y.mp3", "01 - Y", 0, 200),
		}
		opts.PathPrefix = "Radiohead"
		run()
		Expect(lib.edits).To(HaveKey("1"))
		Expect(lib.edits).NotTo(HaveKey("2"))
	})

	It("skips a File that changed on disk, and goes on", func() {
		lib.mfs = model.MediaFiles{
			track("1", "A/B/01 - X.mp3", "01 - X", 0, 200),
			track("2", "A/B/02 - Y.mp3", "02 - Y", 0, 200),
		}
		lib.failWith["1"] = tagedit.ErrChangedOnDisk
		sum := run()
		Expect(sum.Skipped).To(Equal(1))
		Expect(lib.edits).To(HaveKey("2"))
	})

	Describe("with MusicBrainz", func() {
		var mb *fakeMB

		BeforeEach(func() {
			rel := release([]string{"Airbag", "Paranoid Android", "Subterranean Homesick Alien"})
			rel.ArtistCredit = credit("Radiohead", "a-rh")
			rel.ReleaseGroup = musicbrainz.ReleaseGroup{ID: "rg", FirstReleaseDate: "1997"}
			mb = &fakeMB{
				found:    []musicbrainz.Release{{ID: "other", Score: 60}, {ID: "rel", Score: 100}},
				releases: map[string]*musicbrainz.Release{"rel": rel},
			}
			opts.MusicBrainz = mb
		})

		It("names Files with no real title from the album's release, and tags the tracks' IDs", func() {
			lib.mfs = model.MediaFiles{
				track("1", "R/OKC/01 Airbag.mp3", "Airbag", 1, 200),
				track("2", "R/OKC/02.mp3", "Track 02", 2, 201),
				track("3", "R/OKC/03 Subterranean.mp3", "Subterranean Homesick Alien", 3, 202),
			}
			sum := run()
			Expect(mb.searches).To(Equal([]string{"Radiohead|OK Computer"}))
			Expect(lib.edits["2"]).To(HaveKeyWithValue("title", []string{"Paranoid Android"}))
			Expect(lib.edits["1"]).To(HaveKeyWithValue("musicbrainz_trackid", []string{"rec1-1"}))
			Expect(lib.edits["1"]).NotTo(HaveKey("title"))
			Expect(sum.FromMusicBrainz).To(Equal(3))
		})

		It("prefers the release with as many tracks as the folder", func() {
			bigger := release([]string{"Airbag", "Paranoid Android", "Subterranean Homesick Alien", "Bonus"})
			bigger.ID = "bigger"
			mb.releases["bigger"] = bigger
			mb.found = []musicbrainz.Release{{ID: "bigger", Score: 100}, {ID: "rel", Score: 95}}
			lib.mfs = model.MediaFiles{
				track("1", "R/OKC/01 Airbag.mp3", "Airbag", 1, 200),
				track("2", "R/OKC/02.mp3", "Track 02", 2, 201),
				track("3", "R/OKC/03 Subterranean.mp3", "Subterranean Homesick Alien", 3, 202),
			}
			run()
			Expect(report.String()).To(ContainSubstring("musicbrainz release rel\t"))
			Expect(report.String()).NotTo(ContainSubstring("release bigger"))
		})

		It("names the closest release when none matches", func() {
			mb.found = []musicbrainz.Release{{ID: "rel", Score: 100, Title: "OK Computer", ArtistCredit: credit("Radiohead", "a-rh")}}
			lib.mfs = model.MediaFiles{
				track("1", "R/OKC/01 Creep.mp3", "Creep", 0, 200),
				track("2", "R/OKC/02 Karma.mp3", "Karma Police", 2, 201),
			}
			run()
			Expect(report.String()).To(ContainSubstring("closest: OK Computer by Radiohead, release rel"))
		})

		It("reports where MusicBrainz differs from a File it changes nothing in", func() {
			lib.mfs = model.MediaFiles{
				track("1", "R/OKC/01 Airbag.mp3", "Airbag", 1, 200),
				track("2", "R/OKC/02 Paranoid Android.mp3", "Paranoid Android", 2, 201),
				track("3", "R/OKC/03 Subterranean.mp3", "Subterranean Homesick Alien", 0, 202),
			}
			lib.mfs[0].Year = 2001
			lib.mfs[0].MbzRecordingID = "rec1-1"
			run()
			Expect(report.String()).To(ContainSubstring("noted\tR/OKC/01 Airbag.mp3\t\tmusicbrainz release rel\tkept year 2001"))
		})

		It("asks nothing for an album with nothing to repair", func() {
			lib.mfs = model.MediaFiles{track("1", "R/OKC/01 Airbag.mp3", "Airbag", 1, 200)}
			run()
			Expect(mb.searches).To(BeEmpty())
		})

		It("changes nothing on a second run", func() {
			lib.mfs = model.MediaFiles{
				track("1", "R/OKC/01 - Airbag.mp3", "01 - Airbag", 0, 200),
				track("2", "R/OKC/02.mp3", "Track 02", 0, 201),
				track("3", "R/OKC/03 Subterranean.mp3", "Subterranean Homesick Alien", 3, 202),
			}
			run()
			lib.edits = map[string]tagedit.Values{}
			mb.searches = nil
			sum := run()
			Expect(lib.edits).To(BeEmpty())
			Expect(sum.Changed).To(Equal(0))
			Expect(mb.searches).To(BeEmpty())
		})
	})

	It("flags what it couldn't settle", func() {
		lib.mfs = model.MediaFiles{track("1", "A/B/13.mp3", "13", 1, 200)}
		sum := run()
		Expect(sum.Flagged).To(Equal(1))
		Expect(strings.Split(report.String(), "\n")[1]).To(HavePrefix("flagged\tA/B/13.mp3\t\tlocal\tno-title"))
	})
})

var _ = Describe("DirStore", func() {
	It("keeps answers across Stores on the same folder", func() {
		dir := GinkgoT().TempDir()
		DirStore{Dir: dir}.Set("https://musicbrainz.org/ws/2/release/x", []byte("{}"))
		b, ok := DirStore{Dir: dir}.Get("https://musicbrainz.org/ws/2/release/x")
		Expect(ok).To(BeTrue())
		Expect(string(b)).To(Equal("{}"))
		_, ok = DirStore{Dir: dir}.Get("other")
		Expect(ok).To(BeFalse())
	})
})
