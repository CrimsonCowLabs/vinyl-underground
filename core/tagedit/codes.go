package tagedit

import (
	"errors"

	"github.com/navidrome/navidrome/core/safefile"
	"github.com/navidrome/navidrome/model"
)

// CodeInternal is the code of an error the admin can't act on (see Code).
const CodeInternal = "internal"

// Code is the code an edit's error is shown with, which the UI translates: why a File wasn't
// written. Errors with no code of their own are CodeInternal.
func Code(err error) string {
	switch {
	case errors.Is(err, model.ErrNotFound):
		return "notFound"
	case errors.Is(err, ErrEditingOff):
		return "editingOff"
	case errors.Is(err, ErrChangedOnDisk):
		return "changedOnDisk"
	case errors.Is(err, ErrInvalidTags):
		return "invalid"
	case errors.Is(err, ErrUnsupportedFormat):
		return "unsupportedFormat"
	case errors.Is(err, ErrNotVerified):
		return "notVerified"
	case errors.Is(err, safefile.ErrLinked):
		return "linked"
	case errors.Is(err, ErrAlreadyUndone):
		return "alreadyUndone"
	case errors.Is(err, ErrUnfinished):
		return "unfinished"
	case errors.Is(err, ErrTagsChanged):
		return "tagsChanged"
	case errors.Is(err, ErrChangedLater):
		return "changedLater"
	case errors.Is(err, ErrNotUndoable):
		return "notUndoable"
	case errors.Is(err, ErrOldCopyLeft):
		return "oldCopyLeft"
	case errors.Is(err, ErrInvalidImage):
		return "invalidImage"
	case errors.Is(err, ErrNoAlbumFolder):
		return "noAlbumFolder"
	case errors.Is(err, ErrSharedFolder):
		return "sharedFolder"
	case errors.Is(err, ErrCoverChanged):
		return "coverChanged"
	case errors.Is(err, ErrNoFolderCover):
		return "noFolderCover"
	case errors.Is(err, safefile.ErrDestTaken):
		return "pathTaken"
	case errors.Is(err, safefile.ErrCaseTwin):
		return "caseTwin"
	case errors.Is(err, safefile.ErrOutside):
		return "outside"
	}
	return CodeInternal
}
