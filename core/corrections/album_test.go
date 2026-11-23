package corrections_test

import (
	"context"

	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/core/musicbrainz"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func addTrack(id, albumID string, f *model.Folder) {
	GinkgoHelper()
	ctx := request.WithUser(context.Background(), model.User{ID: "admin-id", UserName: "admin", IsAdmin: true})
	mf := model.MediaFile{ID: id, LibraryID: 1, FolderID: f.ID, AlbumID: albumID, Path: f.Path + "/" + f.Name + "/" + id + ".mp3",
		Participants: model.Participants{}}
	Expect(persistence.New(db.Db()).MediaFile(ctx).Put(&mf)).To(Succeed())
}

// fakePickedCovers stands in for the Cover Art Archive: the stored cover of each release it has.
type fakePickedCovers struct {
	covers map[string]artwork.StoredCover
	err    error
	asked  []string
}

func (f *fakePickedCovers) fetch(_ context.Context, releaseID, releaseGroupID string) (artwork.StoredCover, error) {
	f.asked = append(f.asked, releaseID+"|"+releaseGroupID)
	if f.err != nil {
		return artwork.StoredCover{}, f.err
	}
	c, ok := f.covers[releaseID]
	if !ok {
		return artwork.StoredCover{}, musicbrainz.ErrNoCover
	}
	return c, nil
}
