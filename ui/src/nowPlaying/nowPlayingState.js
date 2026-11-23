// Whether Now Playing is open. Its form follows the window's width: a side
// panel docked on the right from PANEL_QUERY up, full screen below it, so
// openNowPlaying() opens it without knowing the width.
export const NOW_PLAYING_OPEN = 'NOW_PLAYING_OPEN'
export const NOW_PLAYING_CLOSE = 'NOW_PLAYING_CLOSE'
export const NOW_PLAYING_TOGGLE = 'NOW_PLAYING_TOGGLE'
export const NOW_PLAYING_TAB = 'NOW_PLAYING_TAB'
export const NOW_PLAYING_TOGGLE_TAB = 'NOW_PLAYING_TOGGLE_TAB'

// Narrower than this, the side panel would leave the page too little room
export const PANEL_QUERY = '(min-width:1000px)'
export const PANEL_WIDTH = 400

// The tabs over Now Playing's art area, in order. Art is labelled Visuals
// while a visual plays there (#96).
export const NOW_PLAYING_TABS = ['art', 'lyrics', 'queue']
const DEFAULT_TAB = 'art'
// The ids tying each tab to the panel it shows
export const tabId = (tab) => `now-playing-tab-${tab}`
export const panelId = 'now-playing-tabpanel'
const validTab = (tab) => (NOW_PLAYING_TABS.includes(tab) ? tab : DEFAULT_TAB)

export const openNowPlaying = () => ({ type: NOW_PLAYING_OPEN })
export const closeNowPlaying = () => ({ type: NOW_PLAYING_CLOSE })
export const toggleNowPlaying = () => ({ type: NOW_PLAYING_TOGGLE })
// showNowPlayingTab shows the tab; toggleNowPlayingTab shows it, or goes
// back to Art if it is showing already, as the toolbar's Lyrics and Queue do.
export const showNowPlayingTab = (tab) => ({ type: NOW_PLAYING_TAB, tab })
export const toggleNowPlayingTab = (tab) => ({
  type: NOW_PLAYING_TOGGLE_TAB,
  tab,
})

export const nowPlayingReducer = (
  state = { open: false, tab: DEFAULT_TAB },
  action,
) => {
  switch (action.type) {
    case NOW_PLAYING_OPEN:
      return { ...state, open: true }
    case NOW_PLAYING_CLOSE:
      return { ...state, open: false }
    case NOW_PLAYING_TOGGLE:
      return { ...state, open: !state.open }
    case NOW_PLAYING_TAB:
      return { ...state, tab: validTab(action.tab) }
    case NOW_PLAYING_TOGGLE_TAB:
      return {
        ...state,
        tab: state.tab === action.tab ? DEFAULT_TAB : validTab(action.tab),
      }
    default:
      return state
  }
}

export const isNowPlayingOpen = (state) => !!state.nowPlaying?.open
export const nowPlayingTab = (state) => validTab(state.nowPlaying?.tab)

// Whether it is open, and the tab it shows, are kept per device, so a panel
// left open is there again after a reload. Full screen never reopens by
// itself: a phone, or a narrow window, starts on the page.
export const savedNowPlaying = (nowPlaying) => ({
  open: !!nowPlaying?.open,
  tab: validTab(nowPlaying?.tab),
})
export const restoredNowPlaying = (saved) => ({
  open: !!saved?.open && !!window.matchMedia?.(PANEL_QUERY).matches,
  tab: validTab(saved?.tab),
})
