package organize

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOrganize(t *testing.T) {
	tests.Init(t, false)
	log.SetLevel(log.LevelFatal)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Organize Suite")
}

// Organize is tested on a real database (Tracks, folders, the journal and the job queue) and real
// Files; db.Db() is a process-wide singleton, so the suite shares one.
var _ = BeforeSuite(func() {
	DeferCleanup(configtest.SetupConfig())
	conf.Server.DbPath = filepath.Join(GinkgoT().TempDir(), "organize.db") + "?_journal_mode=WAL"
	DeferCleanup(db.Init(context.Background()))
})

// library is a library on a temp folder, in a clean database, with editing on.
type library struct {
	ctx context.Context
	ds  model.DataStore
	dir string
	lib model.Library
}

func newLibrary() *library {
	GinkgoHelper()
	l := &library{ctx: request.WithUser(context.Background(), model.User{ID: "admin-1", UserName: "admin", IsAdmin: true}),
		ds: persistence.New(db.Db()), dir: GinkgoT().TempDir()}
	for _, table := range []string{"media_file", "folder", "album", "edit_journal", "edit_job_step", "edit_job"} {
		_, err := db.Db().Exec("delete from " + table)
		Expect(err).ToNot(HaveOccurred())
	}
	_, err := db.Db().Exec("update library set path = ? where id = 1", l.dir)
	Expect(err).ToNot(HaveOccurred())
	l.lib = model.Library{ID: 1, Path: l.dir}
	Expect(l.ds.Folder(l.ctx).Put(model.NewFolder(l.lib, "."))).To(Succeed())
	DeferCleanup(configtest.SetupConfig())
	conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())
	core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: 1, Name: "NAS", Writable: true}}})
	DeferCleanup(func() { core.SetLibraryEditing(core.LibraryEditing{}) })
	return l
}

// file writes a File at rel, and records its folders, and an image in its folder's images, as a
// scan would.
func (l *library) file(rel, content string) {
	GinkgoHelper()
	abs := filepath.Join(l.dir, filepath.FromSlash(rel))
	Expect(os.MkdirAll(filepath.Dir(abs), 0o755)).To(Succeed())
	Expect(os.WriteFile(abs, []byte(content), 0o644)).To(Succeed())
	l.folders(path.Dir(rel))
	switch strings.ToLower(path.Ext(rel)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		f, err := l.ds.Folder(l.ctx).Get(model.FolderID(l.lib, path.Dir(rel)))
		Expect(err).ToNot(HaveOccurred())
		f.ImageFiles = append(f.ImageFiles, path.Base(rel))
		Expect(l.ds.Folder(l.ctx).Put(f)).To(Succeed())
	}
}

// folders records the folder rel and those above it.
func (l *library) folders(rel string) {
	GinkgoHelper()
	for d := rel; d != "." && d != ""; d = path.Dir(d) {
		if _, err := l.ds.Folder(l.ctx).Get(model.FolderID(l.lib, d)); err == nil {
			continue
		}
		Expect(l.ds.Folder(l.ctx).Put(model.NewFolder(l.lib, d))).To(Succeed())
	}
}

// track writes a File at rel and records it as the Track mf (its ID, tags), as a scan would.
func (l *library) track(rel string, mf model.MediaFile) *model.MediaFile {
	GinkgoHelper()
	l.file(rel, "audio of "+rel)
	info, err := os.Stat(filepath.Join(l.dir, filepath.FromSlash(rel)))
	Expect(err).ToNot(HaveOccurred())
	mf.LibraryID, mf.Path, mf.FolderID = 1, rel, model.FolderID(l.lib, path.Dir(rel))
	mf.Size, mf.UpdatedAt, mf.Participants = info.Size(), info.ModTime(), model.Participants{}
	mf.Suffix = path.Ext(rel)[1:]
	if mf.AlbumID == "" {
		mf.AlbumID = mf.AlbumArtist + "|" + mf.Album
	}
	if mf.AlbumArtist != "" && !mf.Compilation {
		mf.HasAlbumArtist = true
	}
	Expect(l.ds.MediaFile(l.ctx).Put(&mf)).To(Succeed())
	return &mf
}

// files is every File in the library, by its path: contents.
func (l *library) files() map[string]string {
	GinkgoHelper()
	res := map[string]string{}
	Expect(filepath.WalkDir(l.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(l.dir, p)
		res[filepath.ToSlash(rel)] = string(b)
		return nil
	})).To(Succeed())
	return res
}

// dirs is every folder in the library.
func (l *library) dirs() []string {
	GinkgoHelper()
	var res []string
	Expect(filepath.WalkDir(l.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == l.dir {
			return err
		}
		rel, _ := filepath.Rel(l.dir, p)
		res = append(res, filepath.ToSlash(rel))
		return nil
	})).To(Succeed())
	return res
}
