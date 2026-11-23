import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createMemoryHistory } from 'history'
import createAdminStore from '../store/createAdminStore'
import {
  closeNowPlaying,
  nowPlayingReducer,
  openNowPlaying,
  showNowPlayingTab,
} from './nowPlayingState'
import { playerReducer } from '../reducers'
import { stationReducer } from '../station/stationState'

const screenWidth = (width) => {
  window.matchMedia = (query) => ({
    media: query,
    matches: width >= Number(/min-width:\s*(\d+)px/.exec(query)[1]),
    addListener: () => {},
    removeListener: () => {},
  })
}

// load starts the app as a page load does, from what the last one saved
const load = () =>
  createAdminStore({
    authProvider: {},
    dataProvider: {},
    history: createMemoryHistory(),
    customReducers: {
      nowPlaying: nowPlayingReducer,
      player: playerReducer,
      station: stationReducer,
    },
  })

const isOpen = (store) => store.getState().nowPlaying.open

describe('whether Now Playing is open, across a reload', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.removeItem('state')
  })
  afterEach(() => {
    vi.useRealTimers()
    delete window.matchMedia
  })

  it('is restored on a wide window', () => {
    screenWidth(1440)
    const store = load()
    store.dispatch(openNowPlaying())
    vi.advanceTimersByTime(1000)

    expect(isOpen(load())).toBe(true)
  })

  it('stays closed after being closed', () => {
    screenWidth(1440)
    const store = load()
    store.dispatch(openNowPlaying())
    store.dispatch(closeNowPlaying())
    vi.advanceTimersByTime(1000)

    expect(isOpen(load())).toBe(false)
  })

  it('never reopens full screen: a phone starts on the page', () => {
    screenWidth(1440)
    const store = load()
    store.dispatch(openNowPlaying())
    vi.advanceTimersByTime(1000)

    screenWidth(390)
    expect(isOpen(load())).toBe(false)
  })
})

describe('the tab Now Playing shows, across a reload', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.removeItem('state')
  })
  afterEach(() => {
    vi.useRealTimers()
    delete window.matchMedia
  })

  it('is remembered, on a phone too', () => {
    screenWidth(390)
    const store = load()
    store.dispatch(showNowPlayingTab('lyrics'))
    vi.advanceTimersByTime(1000)

    expect(load().getState().nowPlaying.tab).toBe('lyrics')
  })

  it('goes back to Art if what was saved is no tab', () => {
    screenWidth(390)
    localStorage.setItem(
      'state',
      JSON.stringify({ nowPlaying: { open: false, tab: 'karaoke' } }),
    )
    expect(load().getState().nowPlaying.tab).toBe('art')
  })
})
