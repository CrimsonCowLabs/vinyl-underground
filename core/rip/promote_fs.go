package rip

import "github.com/navidrome/navidrome/core/safefile"

// promoteFS is every filesystem call Promote makes, so tests can make any of them fail, or
// behave like the case-insensitive NAS. osFS is the real one. Tag edits share it (safefile).
type (
	promoteFS   = safefile.FS
	promoteFile = safefile.File
	osFS        = safefile.OS
)
