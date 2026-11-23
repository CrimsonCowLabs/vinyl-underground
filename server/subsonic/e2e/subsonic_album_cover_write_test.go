package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/Masterminds/squirrel"
	_ "github.com/navidrome/navidrome/adapters/gotaglib" // reads the real Files below
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/metrics"
	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
	"github.com/navidrome/navidrome/server"
	"github.com/navidrome/navidrome/server/events"
	"github.com/navidrome/navidrome/server/nativeapi"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.senan.xyz/taglib"
)

// Choosing or uploading an album cover (#164) writes the album folder's cover.jpg, the real
// scanner sees it at once, and it is undone through the Edit history; Embed in Files writes it
// into the album's Files. These specs run the real database, real Files and the real scanner.
var _ = Describe("Writing an album's cover", func() {
	var (
		nas     model.Library
		nasDir  string
		dir     string
		s       inboxScanner
		admin   model.User
		albumID string
		cover   []byte
	)

	scan := func() {
		Eventually(scanner.IsScanning).Should(BeFalse())
		var err error
		Eventually(func() bool {
			_, err = s.ScanFolders(ctx, false, []model.ScanTarget{{LibraryID: nas.ID}})
			return errors.Is(err, scanner.ErrAlreadyScanning)
		}).Should(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	}
	folderImages := func() []string {
		GinkgoHelper()
		al, err := ds.Album(ctx).Get(albumID)
		Expect(err).ToNot(HaveOccurred())
		folders, err := ds.Folder(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"folder.id": al.FolderIDs}})
		Expect(err).ToNot(HaveOccurred())
		Expect(folders).To(HaveLen(1))
		return folders[0].ImageFiles
	}

	BeforeEach(func() {
		setupTestDB()
		conf.Server.DataFolder = conf.NewDir(GinkgoT().TempDir())

		nasDir = GinkgoT().TempDir()
		dir = filepath.Join(nasDir, "Artist", "Album")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		data, err := os.ReadFile(filepath.Join("tests", "fixtures", "test.mp3"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(dir, "01 Song.mp3"), data, 0o644)).To(Succeed())
		nas = model.Library{Name: "NAS", Path: nasDir}
		Expect(ds.Library(ctx).Put(&nas)).To(Succeed())
		Expect(ds.User(ctx).SetUserLibraries(adminUser.ID, []int{lib.ID, nas.ID})).To(Succeed())
		core.SetLibraryEditing(core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: nas.ID, Name: nas.Name, Writable: true}}})
		DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})

		s = inboxScanner{scanner.New(ctx, ds, events.NoopBroker(),
			playlists.NewPlaylists(ds, artwork.NewUploader(ds)), metrics.NewNoopInstance())}
		scan()
		mfs, err := ds.MediaFile(ctx).GetAll(model.QueryOptions{Filters: squirrel.Eq{"media_file.library_id": nas.ID}})
		Expect(err).ToNot(HaveOccurred())
		Expect(mfs).To(HaveLen(1))
		albumID = mfs[0].AlbumID

		loaded, err := ds.User(ctx).FindByUsername(adminUser.UserName)
		Expect(err).ToNot(HaveOccurred())
		admin = *loaded
		cover, err = os.ReadFile(filepath.Join("tests", "fixtures", "artist", "an-album", "cover.jpg"))
		Expect(err).ToNot(HaveOccurred())
	})

	call := func(method, url, contentType string, body []byte) *httptest.ResponseRecorder {
		editor := tagedit.New(ds, s)
		router := nativeapi.New(ds, core.NewShare(ds), playlists.NewPlaylists(ds, artwork.NewUploader(ds)), nil, tests.NewMockLibraryService(), tests.NewMockUserService(),
			nil, nil, nil, nil, nil, nil, nil, editor)
		queue := editjob.New(ds, nil, editjob.Kinds(ds, editor))
		router.SetEditQueue(queue)
		qctx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { queue.Run(qctx); close(done) }()
		defer func() { stop(); <-done }()
		req := httptest.NewRequest(method, url, bytes.NewBuffer(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		token, err := auth.CreateToken(&admin)
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
		w := httptest.NewRecorder()
		server.JWTVerifier(router).ServeHTTP(w, req)
		return w
	}
	upload := func(image []byte) tagedit.Result {
		GinkgoHelper()
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		Expect(mw.WriteField("albumId", albumID)).To(Succeed())
		fw, err := mw.CreateFormFile("image", "front.jpg")
		Expect(err).ToNot(HaveOccurred())
		_, err = fw.Write(image)
		Expect(err).ToNot(HaveOccurred())
		Expect(mw.Close()).To(Succeed())
		w := call(http.MethodPost, "/correction/albumCoverUpload", mw.FormDataContentType(), buf.Bytes())
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var res tagedit.Result
		Expect(json.Unmarshal(w.Body.Bytes(), &res)).To(Succeed())
		return res
	}

	It("writes an uploaded cover as cover.jpg, which the album's folder shows at once, replacing a chosen cover, and undoes it", func() {
		al, err := ds.Album(ctx).Get(albumID)
		Expect(err).ToNot(HaveOccurred())
		choice := model.Correction{Kind: model.CorrectionAlbumCover, MatchKey: model.AlbumFolderKey(*al),
			From: "Artist – Album", To: "Upload: old.jpg", ImageHash: "0123456789abcdef", ImageMime: "image/jpeg"}
		Expect(ds.Correction(ctx).Add(&choice)).To(Succeed())

		res := upload(cover)
		Expect(res.Changed).To(BeTrue())
		Expect(os.ReadFile(filepath.Join(dir, "cover.jpg"))).To(Equal(cover))
		Expect(folderImages()).To(ConsistOf("cover.jpg"), "the folder was rescanned")
		_, err = ds.Correction(ctx).Get(choice.ID)
		Expect(err).To(MatchError(model.ErrNotFound), "the chosen cover would hide the File")
		Expect(ds.Correction(ctx).GetAll()).To(BeEmpty())
		_, err = ds.ArtworkQueue(ctx).Get(model.KindAlbumArtwork, albumID, model.ImageTypePrimary)
		Expect(err).ToNot(HaveOccurred(), "the album's cover is resolved again")

		w := call(http.MethodPost, "/editHistory/"+res.Entry+"/undo", "", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(filepath.Join(dir, "cover.jpg")).ToNot(BeAnExistingFile())
		Expect(folderImages()).To(BeEmpty())
	})

	It("embeds the folder cover into the album's Files as one job, which is undone as a whole", func() {
		file := filepath.Join(dir, "01 Song.mp3")
		before, err := taglib.ReadImage(file)
		Expect(err).ToNot(HaveOccurred())
		upload(cover)

		w := call(http.MethodPost, "/albumCover/"+albumID+"/embed", "", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		var job model.EditJob
		Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
		Expect(job.Total).To(Equal(1))
		Expect(job.Skipped).To(BeZero())
		Expect(taglib.ReadImage(file)).To(Equal(cover))

		w = call(http.MethodPost, "/editJob/"+job.ID+"/undo", "", nil)
		Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
		Expect(w.Body.String()).To(ContainSubstring(`"skipped":0`))
		Expect(taglib.ReadImage(file)).To(Equal(before))
	})
})
