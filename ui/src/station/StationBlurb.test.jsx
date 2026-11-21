import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { render, screen, act, cleanup, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import config from '../config'
import { playerReducer } from '../reducers'
import { clearQueue, currentPlaying } from '../actions'
import { startStation, stationReducer } from './stationState'
import StationBlurb, { BLURB_MS } from './StationBlurb'

const httpClient = vi.fn()
vi.mock('../dataProvider', () => ({
  httpClient: (...args) => httpClient(...args),
}))

vi.mock('../config', () => ({
  default: { enableStationBlurbs: true },
}))

vi.mock('react-admin', () => ({
  useTranslate: () => (key) => key,
}))

const tracks = (...ids) => ids.map((id) => ({ id, title: id, artist: id }))

const blurbAsks = () =>
  httpClient.mock.calls
    .map(([url]) => url)
    .filter((url) => url.includes('/station/blurb'))

const blurb = (text) => ({ json: { dj: 'Night Owl', text } })
const notWritten = () => ({ json: undefined }) // 204 No Content

describe('StationBlurb', () => {
  let store

  // playing makes the queued Track at idx the one playing.
  const playing = (idx) =>
    act(() => {
      const item = store.getState().player.queue[idx]
      store.dispatch(currentPlaying({ ...item }))
    })

  beforeEach(() => {
    config.enableStationBlurbs = true
    httpClient.mockReset()
    httpClient.mockResolvedValue(notWritten())
    store = createStore(
      combineReducers({ player: playerReducer, station: stationReducer }),
    )
    act(() => {
      store.dispatch(startStation('night-owl', tracks('a', 'b', 'c')))
    })
    render(
      <Provider store={store}>
        <ThemeProvider theme={createTheme()}>
          <StationBlurb />
        </ThemeProvider>
      </Provider>,
    )
  })
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it("shows the DJ's Blurb when the Station changes Track", async () => {
    httpClient.mockResolvedValue(blurb('That was a. Here is b.'))
    playing(0)
    playing(1)

    expect(await screen.findByText('That was a. Here is b.')).toBeTruthy()
    expect(screen.getByText('Night Owl')).toBeTruthy()
    expect(blurbAsks()).toEqual([
      '/api/station/blurb?dj=night-owl&track=b&after=a',
    ])
  })

  it('asks for no Blurb at the Station’s first Track', () => {
    playing(0)
    expect(blurbAsks()).toEqual([])
  })

  it('shows nothing when the Blurb is not written yet', async () => {
    playing(0)
    playing(1)
    await waitFor(() => expect(blurbAsks()).toHaveLength(1))
    expect(screen.queryByText('Night Owl')).toBeNull()
  })

  it('shows nothing when asking for the Blurb fails', async () => {
    httpClient.mockRejectedValue(new Error('offline'))
    playing(0)
    playing(1)
    await waitFor(() => expect(blurbAsks()).toHaveLength(1))
    expect(screen.queryByText('Night Owl')).toBeNull()
  })

  it('drops a Blurb that arrives after the Track changed again', async () => {
    let answer
    httpClient.mockReturnValueOnce(new Promise((r) => (answer = r)))
    playing(0)
    playing(1)
    playing(2)
    await act(async () => answer(blurb('Late for b.')))

    expect(screen.queryByText('Late for b.')).toBeNull()
    expect(blurbAsks()).toEqual([
      '/api/station/blurb?dj=night-owl&track=b&after=a',
      '/api/station/blurb?dj=night-owl&track=c&after=b',
    ])
  })

  it('hides the Blurb after a while', async () => {
    httpClient.mockResolvedValue(blurb('Here is b.'))
    playing(0)
    vi.useFakeTimers()
    playing(1)
    await act(async () => {}) // the Blurb arrives
    expect(screen.getByText('Here is b.')).toBeTruthy()

    act(() => vi.advanceTimersByTime(BLURB_MS + 1000))
    expect(screen.queryByText('Here is b.')).toBeNull()
  })

  it('asks for no Blurb once the Station has ended', () => {
    playing(0)
    act(() => {
      store.dispatch(clearQueue())
      store.dispatch(currentPlaying({ uuid: 'x', trackId: 'x' }))
    })
    expect(blurbAsks()).toEqual([])
  })

  it('asks for no Blurb without an LLM', () => {
    config.enableStationBlurbs = false
    playing(0)
    playing(1)
    expect(blurbAsks()).toEqual([])
  })
})
