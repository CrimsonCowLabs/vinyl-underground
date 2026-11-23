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
import { nowPlayingReducer, openNowPlaying } from './nowPlayingState'
import NowPlaying from './NowPlaying'
// Registers the Lyrics and Queue tools, as the app does at load
import './tabTools'

vi.mock('react-admin', () => ({
  useTranslate: () => (key, opts) =>
    opts ? `${key}:${Object.values(opts).join(',')}` : key,
  useGetOne: () => ({ data: undefined, loading: false }),
  useDataProvider: () => ({ getOne: () => Promise.resolve({ data: {} }) }),
  useNotify: () => vi.fn(),
  useRefresh: () => vi.fn(),
}))

// getLyricsBySongId's answer, per Track: lyrics the server finds beside the
// File (an .lrc) as well as embedded ones
let serverLyrics = {}
const httpClient = vi.fn((url) => {
  const id = new URL(url, 'http://x').searchParams.get('id')
  if (!url.includes('getLyricsBySongId')) return Promise.resolve({ json: {} })
  if (serverLyrics[id] === 'offline') return Promise.reject(new Error('down'))
  return Promise.resolve({
    json: {
      'subsonic-response': {
        status: 'ok',
        lyricsList: { structuredLyrics: serverLyrics[id] || [] },
      },
    },
  })
})
vi.mock('../dataProvider', () => ({
  httpClient: (...args) => httpClient(...args),
}))

const syncedLyrics = [
  {
    synced: true,
    line: [
      { start: 0, value: 'Headlights low on the ring road' },
      { start: 30000, value: 'counting the cats-eyes home' },
      { start: 45000, value: 'you had the window down' },
    ],
  },
]
const unsyncedLyrics = [
  {
    synced: false,
    line: [{ value: 'Copper static on the line' }, { value: 'nobody awake' }],
  },
]

const song = (id, extra = {}) => ({
  id,
  title: `Song ${id}`,
  artist: `Artist ${id}`,
  album: 'Small Hours',
  albumId: 'al1',
  duration: 200,
  ...extra,
})

// Stands in for the player package's audio element and the methods it adds
const fakeAudio = () => {
  const audio = new EventTarget()
  return Object.assign(audio, {
    currentTime: 40,
    duration: 200,
    paused: false,
    togglePlay: vi.fn(),
    playNext: vi.fn(),
    playPrev: vi.fn(),
    pause: vi.fn(),
  })
}

// setup queues songs a, b, c and d (or a Station on them), plays the one at
// playing, and opens Now Playing
const setup = ({ songs, station = false, playing = 0 } = {}) => {
  const tracks = songs || ['a', 'b', 'c', 'd'].map((id) => song(id))
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
        ? startStation('night-owl', tracks, '', 'Night Owl')
        : playTracks(Object.fromEntries(tracks.map((t) => [t.id, t]))),
    )
    store.dispatch(
      currentPlaying({ ...store.getState().player.queue[playing] }),
    )
    store.dispatch(openNowPlaying())
  })
  const audio = fakeAudio()
  render(
    <Provider store={store}>
      <Router history={createMemoryHistory()}>
        <ThemeProvider theme={createTheme()}>
          <NowPlaying audioInstance={audio} />
        </ThemeProvider>
      </Router>
    </Provider>,
  )
  const queueNames = () =>
    store.getState().player.queue.map((t) => t.song.title)
  return { store, audio, queueNames }
}

const tab = (name) =>
  screen.getByRole('tab', { name: `nowPlaying.tabs.${name}` })
const panel = () => screen.getByRole('tabpanel')
const tool = (name) =>
  within(screen.getByRole('toolbar')).getByRole('button', {
    name: `nowPlaying.tabs.${name}`,
  })

describe('Now Playing tabs', () => {
  beforeEach(() => {
    serverLyrics = {}
    httpClient.mockClear()
    // Logged in, so Subsonic URLs can be made
    localStorage.setItem('username', 'listener')
    localStorage.setItem('subsonic-token', 'token')
    localStorage.setItem('subsonic-salt', 'salt')
  })
  afterEach(cleanup)

  it('shows Art first, with Lyrics and Queue beside it', () => {
    setup()
    expect(tab('art')).toHaveAttribute('aria-selected', 'true')
    expect(tab('lyrics')).toHaveAttribute('aria-selected', 'false')
    expect(tab('queue')).toHaveAttribute('aria-selected', 'false')
    expect(
      within(panel()).getByRole('img', { name: /nowPlaying.cover/ }),
    ).toBeInTheDocument()
  })

  it('switches tab when one is chosen, and remembers it', () => {
    const { store } = setup()
    fireEvent.click(tab('queue'))
    expect(tab('queue')).toHaveAttribute('aria-selected', 'true')
    expect(store.getState().nowPlaying.tab).toBe('queue')
  })

  it('moves between tabs with the arrow keys', () => {
    setup()
    fireEvent.keyDown(tab('art'), { key: 'ArrowRight' })
    expect(tab('lyrics')).toHaveAttribute('aria-selected', 'true')
    expect(tab('lyrics')).toHaveFocus()
    fireEvent.keyDown(tab('lyrics'), { key: 'ArrowLeft' })
    expect(tab('art')).toHaveAttribute('aria-selected', 'true')
  })

  it("opens a tab from the toolbar's Lyrics and Queue, and back to Art", () => {
    setup()
    fireEvent.click(tool('lyrics'))
    expect(tab('lyrics')).toHaveAttribute('aria-selected', 'true')
    expect(tool('lyrics')).toHaveAttribute('aria-pressed', 'true')

    fireEvent.click(tool('queue'))
    expect(tab('queue')).toHaveAttribute('aria-selected', 'true')
    expect(tool('lyrics')).toHaveAttribute('aria-pressed', 'false')

    fireEvent.click(tool('queue'))
    expect(tab('art')).toHaveAttribute('aria-selected', 'true')
  })

  describe('Lyrics', () => {
    const showLyrics = (opts) => {
      const result = setup(opts)
      fireEvent.click(tab('lyrics'))
      return result
    }

    it('highlights the synced line being sung', async () => {
      showLyrics({
        songs: [song('a', { lyrics: JSON.stringify(syncedLyrics) })],
      })
      // the audio is at 40s: the second line started at 30s
      const line = await within(panel()).findByRole('button', {
        name: 'counting the cats-eyes home',
      })
      expect(line).toHaveAttribute('aria-current', 'true')
      expect(
        within(panel()).getByRole('button', {
          name: 'you had the window down',
        }),
      ).not.toHaveAttribute('aria-current')
    })

    it('follows the Track as it plays', async () => {
      const { audio } = showLyrics({
        songs: [song('a', { lyrics: JSON.stringify(syncedLyrics) })],
      })
      const later = await within(panel()).findByRole('button', {
        name: 'you had the window down',
      })
      act(() => {
        audio.currentTime = 46
        audio.dispatchEvent(new Event('timeupdate'))
      })
      expect(later).toHaveAttribute('aria-current', 'true')
    })

    it('seeks to a synced line when it is tapped', async () => {
      const { audio } = showLyrics({
        songs: [song('a', { lyrics: JSON.stringify(syncedLyrics) })],
      })
      fireEvent.click(
        await within(panel()).findByRole('button', {
          name: 'you had the window down',
        }),
      )
      expect(audio.currentTime).toBe(45)
    })

    it('shows unsynced lyrics as plain text', async () => {
      showLyrics({
        songs: [song('a', { lyrics: JSON.stringify(unsyncedLyrics) })],
      })
      expect(
        await within(panel()).findByText('Copper static on the line'),
      ).toBeInTheDocument()
      expect(
        within(panel()).getByText('nowPlaying.lyrics.notSynced'),
      ).toBeInTheDocument()
      expect(within(panel()).queryByRole('button')).toBeNull()
    })

    it("shows lyrics the server finds beside the Track's File", async () => {
      serverLyrics = { a: unsyncedLyrics }
      showLyrics({ songs: [song('a')] })
      expect(
        await within(panel()).findByText('nobody awake'),
      ).toBeInTheDocument()
    })

    it('falls back to the embedded lyrics when the server cannot be reached', async () => {
      serverLyrics = { a: 'offline' }
      showLyrics({
        songs: [song('a', { lyrics: JSON.stringify(unsyncedLyrics) })],
      })
      await waitFor(() => expect(httpClient).toHaveBeenCalled())
      expect(
        await within(panel()).findByText('nobody awake'),
      ).toBeInTheDocument()
    })

    it('says so when a Track has no lyrics', async () => {
      showLyrics({ songs: [song('a')] })
      expect(
        await within(panel()).findByText('nowPlaying.lyrics.none'),
      ).toBeInTheDocument()
    })
  })

  describe('Queue', () => {
    const showQueue = (opts) => {
      const result = setup(opts)
      fireEvent.click(tab('queue'))
      return result
    }
    const rows = () => within(panel()).getAllByRole('listitem')
    const play = (title) =>
      within(panel()).getByRole('button', { name: new RegExp(`^${title}`) })

    it('lists the queue, the playing Track marked', () => {
      showQueue({ playing: 1 })
      expect(
        rows().map((r) => within(r).getByText(/^Song/).textContent),
      ).toEqual(['Song a', 'Song b', 'Song c', 'Song d'])
      expect(rows()[1]).toHaveAttribute('aria-current', 'true')
      expect(rows()[0]).not.toHaveAttribute('aria-current')
    })

    it('plays a Track when it is tapped', () => {
      const { store } = showQueue()
      fireEvent.click(play('Song c'))
      expect(store.getState().player.playIndex).toBe(2)
    })

    it('resumes the playing Track when it is tapped while paused', () => {
      const { audio, store } = showQueue()
      audio.paused = true
      act(() => audio.dispatchEvent(new Event('pause')))
      fireEvent.click(play('Song a'))
      expect(audio.togglePlay).toHaveBeenCalled()
      expect(store.getState().player.playIndex).toBeUndefined()
    })

    it('removes a Track', () => {
      const { queueNames } = showQueue()
      fireEvent.click(
        within(panel()).getByRole('button', {
          name: 'nowPlaying.queue.remove:Song c',
        }),
      )
      expect(queueNames()).toEqual(['Song a', 'Song b', 'Song d'])
    })

    it('keeps the last Track, which has no remove button', () => {
      showQueue({ songs: [song('a')] })
      expect(
        within(panel()).queryByRole('button', { name: /queue.remove/ }),
      ).toBeNull()
    })

    it('moves a Track with the arrow keys on its handle', () => {
      const { queueNames } = showQueue()
      const handle = within(panel()).getByRole('button', {
        name: 'nowPlaying.queue.move:Song b',
      })
      fireEvent.keyDown(handle, { key: 'ArrowDown' })
      expect(queueNames()).toEqual(['Song a', 'Song c', 'Song b', 'Song d'])
      fireEvent.keyDown(handle, { key: 'ArrowUp' })
      fireEvent.keyDown(handle, { key: 'ArrowUp' })
      expect(queueNames()).toEqual(['Song b', 'Song a', 'Song c', 'Song d'])
    })

    const swipeDown = (el) => {
      fireEvent.touchStart(el, { touches: [{ clientX: 100, clientY: 100 }] })
      fireEvent.touchMove(el, { touches: [{ clientX: 100, clientY: 300 }] })
      fireEvent.touchEnd(el, {
        changedTouches: [{ clientX: 100, clientY: 300 }],
      })
    }

    it('closes Now Playing with a swipe down while the list is at its top', () => {
      const { store } = showQueue()
      swipeDown(within(panel()).getByText('Song c'))
      expect(store.getState().nowPlaying.open).toBe(false)
    })

    it('scrolls the list rather than closing when it is scrolled down', () => {
      const { store } = showQueue()
      const row = within(panel()).getByText('Song c')
      row.closest('[data-swipe-scroller]').scrollTop = 80
      swipeDown(row)
      expect(store.getState().nowPlaying.open).toBe(true)
    })

    it('drags a Track by its handle rather than closing', () => {
      const { store } = showQueue()
      swipeDown(
        within(panel()).getByRole('button', {
          name: 'nowPlaying.queue.move:Song b',
        }),
      )
      expect(store.getState().nowPlaying.open).toBe(true)
    })

    it("names the DJ picking a Station's Tracks", () => {
      showQueue({ station: true })
      expect(
        within(panel()).getByText('nowPlaying.queue.upNextStation:Night Owl'),
      ).toBeInTheDocument()
    })
  })
})
