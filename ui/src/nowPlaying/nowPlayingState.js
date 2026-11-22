// Whether Now Playing is open. Its form follows the window's width: a side
// panel docked on the right from PANEL_QUERY up, full screen below it, so
// openNowPlaying() opens it without knowing the width.
export const NOW_PLAYING_OPEN = 'NOW_PLAYING_OPEN'
export const NOW_PLAYING_CLOSE = 'NOW_PLAYING_CLOSE'
export const NOW_PLAYING_TOGGLE = 'NOW_PLAYING_TOGGLE'

// Narrower than this, the side panel would leave the page too little room
export const PANEL_QUERY = '(min-width:1000px)'
export const PANEL_WIDTH = 400

export const openNowPlaying = () => ({ type: NOW_PLAYING_OPEN })
export const closeNowPlaying = () => ({ type: NOW_PLAYING_CLOSE })
export const toggleNowPlaying = () => ({ type: NOW_PLAYING_TOGGLE })

export const nowPlayingReducer = (state = { open: false }, action) => {
  switch (action.type) {
    case NOW_PLAYING_OPEN:
      return { ...state, open: true }
    case NOW_PLAYING_CLOSE:
      return { ...state, open: false }
    case NOW_PLAYING_TOGGLE:
      return { ...state, open: !state.open }
    default:
      return state
  }
}

export const isNowPlayingOpen = (state) => !!state.nowPlaying?.open

// Whether it is open is kept per device, so a panel left open is there again
// after a reload. Full screen never reopens by itself: a phone, or a narrow
// window, starts on the page.
export const savedNowPlaying = (nowPlaying) => ({ open: !!nowPlaying?.open })
export const restoredNowPlaying = (saved) => ({
  open: !!saved?.open && !!window.matchMedia?.(PANEL_QUERY).matches,
})
