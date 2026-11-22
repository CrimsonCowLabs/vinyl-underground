package nativeapi

import (
	"errors"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/artwork"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/scanner"
)

func (api *Router) addMetadataRoute(r chi.Router) {
	r.Post("/metadata/{kind}/{id}/refresh", api.refreshMetadata())
}

// refreshMetadata rescans the item's folders and answers once that scan is done, so the UI can
// show the refresh running until then. Its artwork state is cleared deliberately, so a wrong pick
// cannot be served from cache again; external info is re-fetched in the background.
func (api *Router) refreshMetadata() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		kind, _ := model.ParseKind(chi.URLParam(r, "kind"))
		id := chi.URLParam(r, "id")
		if !slices.Contains(artwork.RefreshableKinds, kind) {
			http.Error(w, "invalid artwork kind", http.StatusBadRequest)
			return
		}
		err := api.refresher.Refresh(ctx, kind, id)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, model.ErrNotFound):
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		case errors.Is(err, scanner.ErrAlreadyScanning):
			http.Error(w, "another scan is running, try again later", http.StatusConflict)
		default:
			log.Error(ctx, "Error refreshing", "kind", kind, "id", id, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
}
