package nativeapi

import (
	"net/http"

	"github.com/deluan/rest"
	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core"
)

// addLibraryEditingRoute tells the admin whether editing the Library's Files is on, decided at
// startup (admin only, see routes; ADR-0011).
func (api *Router) addLibraryEditingRoute(r chi.Router) {
	r.Get("/libraryEditing", func(w http.ResponseWriter, r *http.Request) {
		editing := core.CurrentLibraryEditing()
		if editing.Libraries == nil {
			editing.Libraries = []core.LibraryWritable{}
		}
		_ = rest.RespondWithJSON(w, http.StatusOK, editing)
	})
}
