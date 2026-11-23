package metadata_test

import (
	"github.com/navidrome/navidrome/model/metadata"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ReplaceName", func() {
	It("renames a display name of just that Artist, regardless of capitals and spaces", func() {
		Expect(metadata.ReplaceName("greatful   DEAD ", "Greatful Dead", "Grateful Dead")).To(Equal("Grateful Dead"))
	})

	It("replaces the name only where it stands whole, keeping the joiners", func() {
		Expect(metadata.ReplaceName("Joanne & Ann", "Ann", "Anne")).To(Equal("Joanne & Anne"))
		Expect(metadata.ReplaceName("Greatful Dead feat. Bob Dylan", "Greatful Dead", "Grateful Dead")).
			To(Equal("Grateful Dead feat. Bob Dylan"))
		Expect(metadata.ReplaceName("Annabel", "Ann", "Anne")).To(Equal("Annabel"))
	})
})
