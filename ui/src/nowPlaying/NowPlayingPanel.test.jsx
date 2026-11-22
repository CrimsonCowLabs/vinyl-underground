import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { Router } from 'react-router-dom'
import { createMemoryHistory } from 'history'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { playerReducer } from '../reducers'
import { currentPlaying, playTracks } from '../actions'
import { startStation, stationReducer } from '../station/stationState'
import StationBlurb from '../station/StationBlurb'
import {
  closeNowPlaying,
  nowPlayingReducer,
  openNowPlaying,
  toggleNowPlaying,
} from './nowPlayingState'
import NowPlaying from './NowPlaying'
import NowPlayingToggle from './NowPlayingToggle'

const song = {
  id: 't1',
  title: 'Lamplight Freeway',
  artist: 'The Lantern Pilots',
  artistId: 'ar1',
  album: 'Small Hours',
  albumId: 'al1',
  duration: 252,
}

vi.mock('react-admin', () => ({
  useTranslate: () => (key) => key,
  useGetOne: () => ({ data: song, loading: false }),
  useDataProvider: () => ({ getOne: () => Promise.resolve({ data: {} }) }),
  useNotify: () => vi.fn(),
  useRefresh: () => vi.fn(),
}))

const httpClient = vi.fn()
vi.mock('../dataProvider', () => ({
  httpClient: (...args) => httpClient(...args),
}))

vi.mock('../config', async (importOriginal) => {
  const actual = await importOriginal()
  return { default: { ...actual.default, enableStationBlurbs: true } }
})

// screenWidth makes the window that wide, telling the media queries listening
// that it changed, as a resize would
let width = 1440
const listeners = new Set()
const minWidth = (query) => Number(/min-width:\s*(\d+)px/.exec(query)?.[1])
const screenWidth = (w) =>
  act(() => {
    width = w
    listeners.forEach((l) => l())
  })
const mockMatchMedia = () => {
  window.matchMedia = (query) => {
    const mql = {
      media: query,
      get matches() {
        return width >= minWidth(query)
      },
      addListener: (fn) => {
        mql.fn = () => fn(mql)
        listeners.add(mql.fn)
      },
      removeListener: () => listeners.delete(mql.fn),
    }
    return mql
  }
}

const fakeAudio = () =>
  Object.assign(new EventTarget(), {
    currentTime: 40,
    duration: 252,
    paused: false,
    togglePlay: vi.fn(),
    playNext: vi.fn(),
    playPrev: vi.fn(),
    pause: vi.fn(),
  })

const setup = ({ station = false, path = '/album' } = {}) => {
  const store = createStore(
    combineReducers({
      player: playerReducer,
      station: stationReducer,
      nowPlaying: nowPlayingReducer,
    }),
  )
  act(() => {
    store.dispatch(
      station
        ? startStation('night-owl', [song], '', 'Night Owl')
        : playTracks({ [song.id]: song }),
    )
    store.dispatch(currentPlaying({ ...store.getState().player.queue[0] }))
  })
  const history = createMemoryHistory({ initialEntries: [path] })
  render(
    <Provider store={store}>
      <Router history={history}>
        <ThemeProvider theme={createTheme()}>
          <NowPlaying audioInstance={fakeAudio()} />
          <NowPlayingToggle />
          <StationBlurb />
        </ThemeProvider>
      </Router>
    </Provider>,
  )
  const dispatch = (action) => act(() => store.dispatch(action))
  const isOpen = () => store.getState().nowPlaying.open
  return { store, history, dispatch, isOpen }
}

const panel = () =>
  screen.queryByRole('complementary', { name: 'nowPlaying.title' })
const fullScreen = () =>
  screen.queryByRole('dialog', { name: 'nowPlaying.title' })

describe('Now Playing on desktop', () => {
  beforeEach(() => {
    mockMatchMedia()
    width = 1440
    httpClient.mockResolvedValue({ json: undefined })
  })
  afterEach(async () => {
    cleanup()
    listeners.clear()
    delete window.matchMedia
    await waitFor(() => expect(window.history.state?.nowPlaying).toBeFalsy())
  })

  it('opens as a side panel, not full screen', () => {
    const { dispatch } = setup()
    expect(panel()).toBeNull()

    dispatch(openNowPlaying())

    expect(panel()).toBeInTheDocument()
    expect(fullScreen()).toBeNull()
    expect(screen.getByText('Lamplight Freeway')).toBeInTheDocument()
  })

  it('stays open as the Listener moves around the app', () => {
    const { dispatch, history } = setup()
    dispatch(openNowPlaying())

    act(() => history.push('/artist'))
    fireEvent.click(screen.getByRole('link', { name: 'The Lantern Pilots' }))

    expect(history.location.pathname).toBe('/artist/ar1/show')
    expect(panel()).toBeInTheDocument()
    // Nothing for back to close: back goes to the page before
    expect(window.history.state?.nowPlaying).toBeFalsy()
    act(() => history.goBack())
    expect(history.location.pathname).toBe('/artist')
    expect(panel()).toBeInTheDocument()
  })

  it('closes from its own button', () => {
    const { dispatch, isOpen } = setup()
    dispatch(openNowPlaying())

    fireEvent.click(
      within(panel()).getByRole('button', { name: 'nowPlaying.close' }),
    )

    expect(isOpen()).toBe(false)
    expect(panel()).toBeNull()
  })

  it('opens and closes by toggling, as the cover does', () => {
    const { dispatch } = setup()

    dispatch(toggleNowPlaying())
    expect(panel()).toBeInTheDocument()

    dispatch(toggleNowPlaying())
    expect(panel()).toBeNull()
  })

  it("opens and closes from the player bar's button", () => {
    setup()
    const toggle = screen.getByRole('button', { name: 'nowPlaying.open' })
    expect(toggle).toHaveAttribute('aria-pressed', 'false')

    fireEvent.click(toggle)
    expect(panel()).toBeInTheDocument()
    expect(toggle).toHaveAttribute('aria-pressed', 'true')
    expect(toggle).toHaveAccessibleName('nowPlaying.close')

    fireEvent.click(toggle)
    expect(panel()).toBeNull()
  })

  it("shows a Station's DJ", () => {
    const { dispatch } = setup({ station: true })
    dispatch(openNowPlaying())

    expect(panel()).toHaveTextContent('Night Owl')
  })

  describe('below 1000px', () => {
    it('opens full screen instead', () => {
      width = 900
      const { dispatch } = setup()

      dispatch(openNowPlaying())

      expect(fullScreen()).toBeInTheDocument()
      expect(panel()).toBeNull()
    })

    it('switches to full screen when the window narrows, and back', () => {
      const { dispatch, isOpen } = setup()
      dispatch(openNowPlaying())

      screenWidth(999)
      expect(panel()).toBeNull()
      expect(fullScreen()).toBeInTheDocument()

      screenWidth(1000)
      expect(fullScreen()).toBeNull()
      expect(panel()).toBeInTheDocument()
      expect(isOpen()).toBe(true)
    })

    it('stays closed across the threshold when closed', () => {
      const { dispatch } = setup()
      dispatch(closeNowPlaying())

      screenWidth(900)
      expect(fullScreen()).toBeNull()
      screenWidth(1440)
      expect(panel()).toBeNull()
    })
  })
})
