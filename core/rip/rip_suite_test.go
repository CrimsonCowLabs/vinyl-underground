package rip

import (
	"testing"

	_ "github.com/navidrome/navidrome/adapters/gotaglib"  // registers the taglib extractor
	_ "github.com/navidrome/navidrome/core/storage/local" // registers the "file" storage scheme
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRip(t *testing.T) {
	tests.Init(t, false)
	log.SetLevel(log.LevelFatal)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Rip Suite")
}
