import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { playerReducer } from '../reducers'
import { currentPlaying, playTracks } from '../actions'
import { startStation, stationReducer } from '../station/stationState'
import { nowPlayingReducer } from '../nowPlaying/nowPlayingState'
import MiniPlayer from './MiniPlayer'

const song = {
  id: 't1',
  title: 'Lamplight Freeway',
  artist: 'The Lantern Pilots',
  album: 'Small Hours',
  albumId: 'al1',
  duration: 252,
}

vi.mock('react-admin', () => ({
  useTranslate: () => (key) => key,
}))

vi.mock('../dataProvider', () => ({
  httpClient: () => Promise.resolve({ json: undefined }),
}))

// Stands in for the player package's audio element and the methods it adds
const fakeAudio = () => {
  const audio = new EventTarget()
  return Object.assign(audio, {
    currentTime: 63,
    duration: 252,
    paused: false,
    togglePlay: vi.fn(),
    playNext: vi.fn(),
    playPrev: vi.fn(),
  })
}

const radio = {
  id: 'r1',
  name: 'Night FM',
  streamUrl: 'http://radio.example/',
  isRadio: true,
}

// setup plays song (or with radio, an Internet Radio station), or with
// station, starts the Night Owl's Station on song
const setup = ({
  station = false,
  radio: onRadio = false,
  played = true,
} = {}) => {
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
        : playTracks(onRadio ? { [radio.id]: radio } : { [song.id]: song }),
    )
    if (played) {
      store.dispatch(currentPlaying({ ...store.getState().player.queue[0] }))
    }
  })
  const audio = fakeAudio()
  render(
    <Provider store={store}>
      <ThemeProvider theme={createTheme()}>
        <MiniPlayer audioInstance={audio} />
      </ThemeProvider>
    </Provider>,
  )
  const isOpen = () => store.getState().nowPlaying.open
  return { store, audio, isOpen }
}

describe('MiniPlayer', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(cleanup)

  it('shows the cover, title and artist of the Track playing', () => {
    const { store } = setup()

    expect(screen.getByText('Lamplight Freeway')).toBeInTheDocument()
    expect(screen.getByText('The Lantern Pilots')).toBeInTheDocument()
    expect(document.querySelector('img').getAttribute('src')).toBe(
      store.getState().player.queue[0].cover,
    )
    expect(screen.queryByText('Night Owl')).toBeNull()
  })

  it("hints at the DJ's name on a Station", () => {
    setup({ station: true })

    expect(screen.getByText('Night Owl')).toBeInTheDocument()
    expect(screen.getByText('The Lantern Pilots')).toBeInTheDocument()
  })

  it('shows the queued Track before anything has played since a reload', () => {
    setup({ played: false })

    expect(screen.getByText('Lamplight Freeway')).toBeInTheDocument()
  })

  it('pauses, plays and skips to the next Track, without opening Now Playing', () => {
    const { audio, isOpen } = setup()

    fireEvent.click(screen.getByRole('button', { name: 'nowPlaying.pause' }))
    expect(audio.togglePlay).toHaveBeenCalledTimes(1)

    audio.paused = true
    act(() => audio.dispatchEvent(new Event('pause')))
    fireEvent.click(screen.getByRole('button', { name: 'nowPlaying.play' }))
    expect(audio.togglePlay).toHaveBeenCalledTimes(2)

    fireEvent.click(screen.getByRole('button', { name: 'nowPlaying.next' }))
    expect(audio.playNext).toHaveBeenCalledTimes(1)

    expect(isOpen()).toBe(false)
  })

  it('opens Now Playing when tapped anywhere but its buttons', () => {
    const { isOpen, store } = setup({ station: true })

    fireEvent.click(screen.getByText('Night Owl'))
    expect(isOpen()).toBe(true)

    act(() => store.dispatch({ type: 'NOW_PLAYING_CLOSE' }))
    fireEvent.click(document.querySelector('img'))
    expect(isOpen()).toBe(true)
  })

  it('opens Now Playing from its labelled button, for the keyboard', () => {
    const { isOpen } = setup()

    fireEvent.click(screen.getByRole('button', { name: 'nowPlaying.open' }))

    expect(isOpen()).toBe(true)
  })

  it('draws how far through the Track playback is, and follows it', () => {
    const { audio } = setup()
    const progress = screen.getByRole('progressbar', {
      name: 'nowPlaying.progress',
    })
    expect(progress).toHaveAttribute('aria-valuenow', '25') // 63 s of 252

    audio.currentTime = 126
    act(() => audio.dispatchEvent(new Event('timeupdate')))

    expect(progress).toHaveAttribute('aria-valuenow', '50')
  })

  it('draws no progress for Internet Radio, a stream with no end', () => {
    setup({ radio: true })

    expect(screen.getByText('Night FM')).toBeInTheDocument()
    expect(screen.queryByRole('progressbar')).toBeNull()
  })
})
