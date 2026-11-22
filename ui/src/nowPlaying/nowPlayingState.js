// Whether Now Playing is open. It isn't kept across reloads: a phone starts
// on the page, with the mini player.
export const NOW_PLAYING_OPEN = 'NOW_PLAYING_OPEN'
export const NOW_PLAYING_CLOSE = 'NOW_PLAYING_CLOSE'

export const openNowPlaying = () => ({ type: NOW_PLAYING_OPEN })
export const closeNowPlaying = () => ({ type: NOW_PLAYING_CLOSE })

export const nowPlayingReducer = (state = { open: false }, action) => {
  switch (action.type) {
    case NOW_PLAYING_OPEN:
      return { ...state, open: true }
    case NOW_PLAYING_CLOSE:
      return { ...state, open: false }
    default:
      return state
  }
}
