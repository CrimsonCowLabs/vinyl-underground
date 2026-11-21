package stations_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStations(t *testing.T) {
	tests.Init(t, false)
	log.SetLevel(log.LevelFatal)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Stations Suite")
}

// db.Db() is a process-wide singleton, so the whole suite shares one database.
var _ = BeforeSuite(func() {
	DeferCleanup(configtest.SetupConfig())
	conf.Server.DbPath = filepath.Join(GinkgoT().TempDir(), "stations.db") + "?_journal_mode=WAL"
	DeferCleanup(db.Init(context.Background()))
})
