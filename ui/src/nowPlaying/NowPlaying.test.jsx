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
} from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { playerReducer } from '../reducers'
import { currentPlaying, playTracks } from '../actions'
import subsonic from '../subsonic'
import { startStation, stationReducer } from '../station/stationState'
import StationBlurb from '../station/StationBlurb'
import {
  closeNowPlaying,
  nowPlayingReducer,
  openNowPlaying,
} from './nowPlayingState'
import NowPlaying from './NowPlaying'
import { registerNowPlayingTool } from './toolbarSlots'
import { NowPlayingToolButton } from './NowPlayingToolbar'

const song = {
  id: 't1',
  title: 'Lamplight Freeway',
  artist: 'The Lantern Pilots',
  artistId: 'ar1',
  album: 'Small Hours',
  albumId: 'al1',
  duration: 252,
  starred: false,
  rating: 0,
}

// The record the server answers for the Track playing
let serverSong
vi.mock('react-admin', () => ({
  useTranslate: () => (key, opts) =>
    opts?.smart_count != null ? `${key}:${opts.smart_count}` : key,
  useGetOne: () => ({ data: serverSong, loading: false }),
  useDataProvider: () => ({ getOne: () => Promise.resolve({ data: {} }) }),
  useNotify: () => vi.fn(),
  useRefresh: () => vi.fn(),
}))

vi.mock('../subsonic', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    default: {
      ...actual.default,
      star: vi.fn(() => Promise.resolve()),
      unstar: vi.fn(() => Promise.resolve()),
      setRating: vi.fn(() => Promise.resolve()),
    },
  }
})

const httpClient = vi.fn()
vi.mock('../dataProvider', () => ({
  httpClient: (...args) => httpClient(...args),
}))

vi.mock('../config', async (importOriginal) => {
  const actual = await importOriginal()
  return { default: { ...actual.default, enableStationBlurbs: true } }
})

// Stands in for the player package's audio element and the methods it adds
const fakeAudio = () => {
  const audio = new EventTarget()
  return Object.assign(audio, {
    currentTime: 40,
    duration: 252,
    paused: false,
    togglePlay: vi.fn(),
    playNext: vi.fn(),
    playPrev: vi.fn(),
    pause: vi.fn(),
  })
}

// setup plays song, or with station, starts the Night Owl's Station on
// songs b, c and d
const setup = ({ station = false } = {}) => {
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
        ? startStation(
            'night-owl',
            ['b', 'c', 'd'].map((id) => ({ ...song, id, title: `Song ${id}` })),
            '',
            'Night Owl',
          )
        : playTracks({ [song.id]: song }),
    )
    store.dispatch(currentPlaying({ ...store.getState().player.queue[0] }))
  })
  const history = createMemoryHistory({ initialEntries: ['/album'] })
  const audio = fakeAudio()
  render(
    <Provider store={store}>
      <Router history={history}>
        <ThemeProvider theme={createTheme()}>
          <NowPlaying audioInstance={audio} />
          <StationBlurb />
        </ThemeProvider>
      </Router>
    </Provider>,
  )
  const open = () => act(() => store.dispatch(openNowPlaying()))
  const isOpen = () => store.getState().nowPlaying.open
  // playing makes the queued Track at idx the one playing
  const playing = (idx) =>
    act(() => {
      store.dispatch(currentPlaying({ ...store.getState().player.queue[idx] }))
    })
  return { store, history, audio, open, isOpen, playing }
}

const view = () => screen.queryByRole('dialog', { name: 'nowPlaying.title' })

describe('NowPlaying', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    httpClient.mockResolvedValue({ json: undefined }) // no Blurb written
    serverSong = { ...song }
  })
  afterEach(async () => {
    cleanup()
    // Let the view take its history entry back before the next test
    await waitFor(() => expect(window.history.state?.nowPlaying).toBeFalsy())
    await new Promise((resolve) => setTimeout(resolve, 0))
  })

  it('opens full screen with the Track playing', () => {
    const { open } = setup()
    expect(view()).toBeNull()

    open()

    expect(view()).toBeInTheDocument()
    expect(screen.getByText('Lamplight Freeway')).toBeInTheDocument()
  })

  it('closes from its close button, and playback carries on', async () => {
    const { open, isOpen, audio } = setup()
    open()

    fireEvent.click(screen.getByRole('button', { name: 'nowPlaying.close' }))

    expect(isOpen()).toBe(false)
    await waitFor(() => expect(view()).toBeNull())
    expect(audio.pause).not.toHaveBeenCalled()
    expect(audio.togglePlay).not.toHaveBeenCalled()
  })

  it('closes with Esc', () => {
    const { open, isOpen } = setup()
    open()

    fireEvent.keyDown(view(), { key: 'Escape' })

    expect(isOpen()).toBe(false)
  })

  it('closes with back rather than leaving the page', async () => {
    const { open, isOpen } = setup()
    const before = window.location.href
    open()

    act(() => window.history.back())

    await waitFor(() => expect(isOpen()).toBe(false))
    expect(window.location.href).toBe(before)
  })

  it('takes back its history entry when closed another way', async () => {
    const { open, isOpen } = setup()
    open()
    expect(window.history.state?.nowPlaying).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: 'nowPlaying.close' }))
    await waitFor(() => expect(window.history.state?.nowPlaying).toBeFalsy())

    // Reopened at once, before that back lands, it still closes with back
    open()
    fireEvent.click(screen.getByRole('button', { name: 'nowPlaying.close' }))
    open()
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(isOpen()).toBe(true)
    expect(window.history.state?.nowPlaying).toBe(true)
    act(() => window.history.back())
    await waitFor(() => expect(isOpen()).toBe(false))
  })

  it('closes with a down gesture', () => {
    const { open, isOpen } = setup()
    open()
    const header = screen.getByRole('button', { name: 'nowPlaying.close' })

    fireEvent.touchStart(header, { touches: [{ clientX: 100, clientY: 100 }] })
    fireEvent.touchMove(header, { touches: [{ clientX: 104, clientY: 260 }] })
    fireEvent.touchEnd(header, {
      changedTouches: [{ clientX: 104, clientY: 260 }],
    })

    expect(isOpen()).toBe(false)
  })

  it.each([
    ['title', 'Lamplight Freeway', '/album/al1/show'],
    ['artist', 'The Lantern Pilots', '/artist/ar1/show'],
    ['album', 'Small Hours', '/album/al1/show'],
  ])('goes to the %s, closing the view', async (_, name, path) => {
    const { open, isOpen, history } = setup()
    open()

    fireEvent.click(screen.getByRole('link', { name }))

    expect(history.location.pathname).toBe(path)
    expect(isOpen()).toBe(false)
    // The view's history entry became the destination, nothing to take back
    expect(window.history.state?.nowPlaying).toBeFalsy()
  })

  describe('star and rating', () => {
    const starButton = () =>
      screen.getByRole('button', { name: 'nowPlaying.star' })

    it('stars the Track, drawn as an outlined star until it is', () => {
      const { open } = setup()
      open()

      expect(starButton()).toHaveAttribute('aria-pressed', 'false')
      expect(screen.getByTestId('star-outlined')).toBeInTheDocument()
      fireEvent.click(starButton())

      expect(subsonic.star).toHaveBeenCalledWith('t1')
    })

    it('unstars a starred Track, drawn as a filled star', () => {
      serverSong = { ...song, starred: true }
      const { open } = setup()
      open()

      expect(starButton()).toHaveAttribute('aria-pressed', 'true')
      expect(screen.getByTestId('star-filled')).toBeInTheDocument()
      fireEvent.click(starButton())

      expect(subsonic.unstar).toHaveBeenCalledWith('t1')
    })

    it('rates the Track', () => {
      serverSong = { ...song, rating: 2 }
      const { open } = setup()
      open()

      expect(
        screen.getByRole('radio', { name: 'nowPlaying.rating:2' }),
      ).toBeChecked()
      fireEvent.click(
        screen.getByRole('radio', { name: 'nowPlaying.rating:4' }),
      )

      expect(subsonic.setRating).toHaveBeenCalledWith('t1', 4)
    })

    it('uses the Track as queued when the server is out of reach', () => {
      serverSong = undefined // a Download played offline
      const { open } = setup()
      open()

      expect(screen.getByText('Lamplight Freeway')).toBeInTheDocument()
      expect(starButton()).toHaveAttribute('aria-pressed', 'false')
    })
  })

  describe('on a Station', () => {
    const blurb = (text) => ({ json: { djName: 'Night Owl', text } })
    const card = () => screen.getByRole('region', { name: 'nowPlaying.djCard' })

    it("shows the DJ's name in a card", () => {
      const { open } = setup({ station: true })
      open()

      expect(screen.getByText('nowPlaying.station')).toBeInTheDocument()
      expect(card()).toHaveTextContent('Night Owl')
    })

    it("shows the DJ's latest Blurb in the card, updating as Tracks change", async () => {
      const { open, playing } = setup({ station: true })
      open()

      httpClient.mockResolvedValue(blurb('That was b. Here is c.'))
      playing(1)
      await waitFor(() =>
        expect(card()).toHaveTextContent('That was b. Here is c.'),
      )
      // Shown in the card, not in a pop-up over the view as well
      expect(screen.getAllByText('That was b. Here is c.')).toHaveLength(1)

      httpClient.mockResolvedValue(blurb('Now d.'))
      playing(2)
      expect(card()).not.toHaveTextContent('That was b. Here is c.')
      await waitFor(() => expect(card()).toHaveTextContent('Now d.'))
    })

    it('has no DJ card off a Station', () => {
      const { open } = setup()
      open()

      expect(
        screen.queryByRole('region', { name: 'nowPlaying.djCard' }),
      ).toBeNull()
      expect(
        screen.getByText('nowPlaying.playingFromAlbum'),
      ).toBeInTheDocument()
    })
  })

  describe('playback', () => {
    it('shows the time played and the time left', () => {
      const { open } = setup()
      open()

      expect(screen.getByText('0:40')).toBeInTheDocument()
      expect(screen.getByText('-3:32')).toBeInTheDocument()
    })

    it("drives the player package's playback and queue", () => {
      const { open, audio } = setup()
      open()

      fireEvent.click(screen.getByRole('button', { name: 'nowPlaying.pause' }))
      fireEvent.click(screen.getByRole('button', { name: 'nowPlaying.next' }))
      fireEvent.click(
        screen.getByRole('button', { name: 'nowPlaying.previous' }),
      )

      expect(audio.togglePlay).toHaveBeenCalledTimes(1)
      expect(audio.playNext).toHaveBeenCalledTimes(1)
      expect(audio.playPrev).toHaveBeenCalledTimes(1)
    })

    it('follows the audio as it plays and pauses', () => {
      const { open, audio } = setup()
      open()

      act(() => {
        audio.paused = true
        audio.currentTime = 61
        audio.dispatchEvent(new Event('pause'))
      })

      expect(
        screen.getByRole('button', { name: 'nowPlaying.play' }),
      ).toBeInTheDocument()
      expect(screen.getByText('1:01')).toBeInTheDocument()
    })
  })

  describe('toolbar', () => {
    const toolbar = () =>
      screen.queryByRole('toolbar', { name: 'nowPlaying.tools' })
    let unregister = []
    afterEach(() => {
      unregister.forEach((fn) => fn())
      unregister = []
    })

    it('is left out while no feature has registered a tool', () => {
      const { open } = setup()
      open()

      expect(toolbar()).toBeNull()
    })

    it('shows the registered tools in slot order, hiding the others', () => {
      const tool = (label) => {
        const Tool = () => <NowPlayingToolButton icon={null} label={label} />
        return Tool
      }
      unregister = [
        registerNowPlayingTool('speed', tool('Speed')),
        registerNowPlayingTool('lyrics', tool('Lyrics')),
      ]
      const { open } = setup()
      open()

      const buttons = Array.from(toolbar().querySelectorAll('button'))
      expect(buttons.map((b) => b.getAttribute('aria-label'))).toEqual([
        'Lyrics',
        'Speed',
      ])
    })
  })

  it('stays open after a short or sideways drag', () => {
    const { open, isOpen } = setup()
    open()
    const header = screen.getByRole('button', { name: 'nowPlaying.close' })

    fireEvent.touchStart(header, { touches: [{ clientX: 100, clientY: 100 }] })
    fireEvent.touchMove(header, { touches: [{ clientX: 300, clientY: 140 }] })
    fireEvent.touchEnd(header, {
      changedTouches: [{ clientX: 300, clientY: 140 }],
    })

    expect(isOpen()).toBe(true)
  })
})
