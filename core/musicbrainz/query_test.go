package musicbrainz_test

import (
	"strings"

	"github.com/navidrome/navidrome/core/musicbrainz"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ParseQuery", func() {
	const mbid = "5e8cb3d5-0a4e-4b3c-9c56-1d2f3a4b5c6d"

	DescribeTable("works out what the admin typed",
		func(typed string, kind musicbrainz.QueryKind, value string) {
			q := musicbrainz.ParseQuery(typed)
			Expect(q.Kind).To(Equal(kind))
			Expect(q.Value).To(Equal(value))
			Expect(q.Text).To(Equal(strings.TrimSpace(typed)), "as typed, for the fallback searches")
		},
		Entry("a release link", "https://musicbrainz.org/release/"+mbid, musicbrainz.QueryRelease, mbid),
		Entry("a release link with a trailing path", "https://musicbrainz.org/release/"+mbid+"/cover-art",
			musicbrainz.QueryRelease, mbid),
		Entry("a release link with a query, in capitals, on beta, without a scheme",
			" beta.MusicBrainz.org/release/"+"5E8CB3D5-0A4E-4B3C-9C56-1D2F3A4B5C6D?tab=x ", musicbrainz.QueryRelease, mbid),
		Entry("a bare release MBID", " "+mbid+" ", musicbrainz.QueryRelease, mbid),
		Entry("a link to anything else on MusicBrainz", "https://musicbrainz.org/release-group/"+mbid,
			musicbrainz.QueryOtherLink, "https://musicbrainz.org/release-group/"+mbid),
		Entry("a link elsewhere", "https://www.discogs.com/release/123-House-Party",
			musicbrainz.QueryOtherLink, "https://www.discogs.com/release/123-House-Party"),
		Entry("a barcode", "768697502725", musicbrainz.QueryBarcode, "768697502725"),
		Entry("a barcode with spaces", "7 68697 50272 5", musicbrainz.QueryBarcode, "768697502725"),
		Entry("a barcode with hyphens", "0-7464-47128-2-8", musicbrainz.QueryBarcode, "074644712828"),
		Entry("an EAN-8", "96385074", musicbrainz.QueryBarcode, "96385074"),
		Entry("a catalog number of digits and hyphens is tried as a barcode first",
			"76869-75027-2", musicbrainz.QueryBarcode, "76869750272"),
		Entry("too few digits for a barcode", "1999", musicbrainz.QueryTitle, "1999"),
		Entry("too many digits for a barcode", "123456789012345", musicbrainz.QueryTitle, "123456789012345"),
		Entry("a catalog number", "WARPCD92", musicbrainz.QueryCatalogNumber, "WARPCD92"),
		Entry("a catalog number with punctuation", "TVT-4010-2", musicbrainz.QueryCatalogNumber, "TVT-4010-2"),
		Entry("a title", " House Party (Volume One) ", musicbrainz.QueryTitle, "House Party (Volume One)"),
		Entry("a one-word title", "Nevermind", musicbrainz.QueryTitle, "Nevermind"),
		Entry("a title with a space and digits", "In Utero 20", musicbrainz.QueryTitle, "In Utero 20"),
		Entry("a title of one digit and letters", "X4", musicbrainz.QueryTitle, "X4"),
		Entry("nothing", "  ", musicbrainz.QueryTitle, ""),
	)
})

var _ = Describe("IsVariousArtistsName", func() {
	DescribeTable("is true for the names libraries give Various Artists",
		func(name string, want bool) {
			Expect(musicbrainz.IsVariousArtistsName(name)).To(Equal(want))
		},
		Entry(nil, "Various", true),
		Entry(nil, "various", true),
		Entry(nil, " VA ", true),
		Entry(nil, "V.A.", true),
		Entry(nil, "v.a", true),
		Entry(nil, "V/A", true),
		Entry(nil, "Various Artist", true),
		Entry(nil, "VARIOUS ARTISTS", true),
		Entry(nil, "Various  Artists", true),
		Entry(nil, "Variously", false),
		Entry(nil, "Various Artists Orchestra", false),
		Entry(nil, "Vanessa", false),
		Entry(nil, "", false),
	)
})

var _ = Describe("ReleaseSearchArtist", func() {
	It("searches a compilation, or an album by Various, as Various Artists", func() {
		Expect(musicbrainz.ReleaseSearchArtist("Various", false)).To(Equal("Various Artists"))
		Expect(musicbrainz.ReleaseSearchArtist("v/a", false)).To(Equal("Various Artists"))
		Expect(musicbrainz.ReleaseSearchArtist("DJ Someone", true)).To(Equal("Various Artists"))
		Expect(musicbrainz.ReleaseSearchArtist("", true)).To(Equal("Various Artists"))
	})

	It("keeps any other album artist as it is", func() {
		Expect(musicbrainz.ReleaseSearchArtist(" Nirvana ", false)).To(Equal("Nirvana"))
		Expect(musicbrainz.ReleaseSearchArtist("", false)).To(Equal(""))
	})
})
