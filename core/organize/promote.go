package organize

import (
	"context"
	"fmt"

	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
)

// PromoteLibrary is the library a Promote (#171) moves the Inbox's reviewed Files into, at their
// scheme path (Scope.Into): the Library, that is the first writable library (by ID) that isn't the
// Inbox. It is nil while no library is writable (editing is off, ADR-0011): Promote is off then.
func PromoteLibrary(ctx context.Context, ds model.DataStore) (*model.Library, error) {
	libs, err := ds.Library(ctx).GetAll(model.QueryOptions{Sort: "id"})
	if err != nil {
		return nil, fmt.Errorf("loading libraries: %w", err)
	}
	inbox, err := core.FindInboxLibrary(ctx, ds)
	if err != nil {
		return nil, err
	}
	for i := range libs {
		if (inbox == nil || libs[i].ID != inbox.ID) && tagedit.Writable(libs[i].ID) {
			return &libs[i], nil
		}
	}
	return nil, nil
}
