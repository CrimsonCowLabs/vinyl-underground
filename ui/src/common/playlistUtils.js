// Only the owner can change a Playlist: a shared Playlist is read-only to every
// other Listener, admins included. The server enforces the same rule.
export const isWritable = (ownerId) => {
  return !!ownerId && localStorage.getItem('userId') === ownerId
}

export const isReadOnly = (ownerId) => {
  return !isWritable(ownerId)
}

export const isSmartPlaylist = (pls) => !!pls.rules

export const canChangeTracks = (pls) =>
  isWritable(pls.ownerId) && !isSmartPlaylist(pls) && !pls.sync
