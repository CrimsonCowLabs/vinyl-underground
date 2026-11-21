import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { render, waitFor, act, cleanup } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { playerReducer } from '../reducers'
import { clearQueue, currentPlaying } from '../actions'
import { startStation, stationReducer } from './stationState'
import { useStationRefill, REFILL_COUNT, RETRY_MS } from './useStationRefill'

const httpClient = vi.fn()
vi.mock('../dataProvider', () => ({
  httpClient: (...args) => httpClient(...args),
}))

const tracks = (...ids) => ids.map((id) => ({ id, title: id, artist: id }))

const Refill = () => {
  useStationRefill()
  return null
}

const newStore = () =>
  createStore(
    combineReducers({ player: playerReducer, station: stationReducer }),
  )

const queuedIds = (store) =>
  store.getState().player.queue.map((item) => item.trackId)

// playing makes the Track at idx the one playing.
const playing = (store, idx) => {
  const item = store.getState().player.queue[idx]
  store.dispatch(currentPlaying({ ...item }))
}

describe('useStationRefill', () => {
  beforeEach(() => {
    httpClient.mockReset()
  })
  afterEach(cleanup)

  it('asks the server for more Tracks before the Station runs out', async () => {
    httpClient.mockResolvedValue({ json: { tracks: tracks('d', 'e', 'f') } })
    const store = newStore()
    store.dispatch(startStation('night-owl', tracks('a', 'b', 'c', 'x', 'y')))
    render(
      <Provider store={store}>
        <Refill />
      </Provider>,
    )
    expect(httpClient).not.toHaveBeenCalled()

    // Two skips leave only two Tracks queued.
    act(() => playing(store, 2))

    await waitFor(() => expect(queuedIds(store)).toHaveLength(8))
    expect(httpClient).toHaveBeenCalledTimes(1)
    const [url, opts] = httpClient.mock.calls[0]
    expect(url).toMatch(/\/station\/next$/)
    expect(opts.method).toBe('POST')
    expect(JSON.parse(opts.body)).toEqual({
      dj: 'night-owl',
      count: REFILL_COUNT,
    })
    expect(queuedIds(store).slice(5)).toEqual(['d', 'e', 'f'])
  })

  it('ends the Station when its DJ is gone (deleted elsewhere), leaving the queue to play out', async () => {
    httpClient.mockRejectedValue(
      Object.assign(new Error('unknownDJ'), {
        status: 404,
        body: { error: 'unknownDJ' },
      }),
    )
    const store = newStore()
    store.dispatch(startStation('dj-1', tracks('a', 'b')))
    render(
      <Provider store={store}>
        <Refill />
      </Provider>,
    )

    await waitFor(() => expect(store.getState().station.djId).toBeNull())
    expect(queuedIds(store)).toEqual(['a', 'b'])
    expect(httpClient).toHaveBeenCalledTimes(1)
  })

  it("asks for the Station's Theme on every refill", async () => {
    httpClient.mockResolvedValue({ json: { tracks: tracks('d', 'e', 'f') } })
    const store = newStore()
    store.dispatch(startStation('night-owl', tracks('a'), 'late-night drive'))
    render(
      <Provider store={store}>
        <Refill />
      </Provider>,
    )
    await waitFor(() => expect(queuedIds(store)).toHaveLength(4))
    expect(JSON.parse(httpClient.mock.calls[0][1].body)).toEqual({
      dj: 'night-owl',
      count: REFILL_COUNT,
      theme: 'late-night drive',
    })
  })

  it('does nothing when no Station is on air', () => {
    const store = newStore()
    render(
      <Provider store={store}>
        <Refill />
      </Provider>,
    )
    expect(httpClient).not.toHaveBeenCalled()
  })

  it('drops a refill that arrives after the Station ended', async () => {
    let answer
    httpClient.mockReturnValue(new Promise((resolve) => (answer = resolve)))
    const store = newStore()
    store.dispatch(startStation('sunny', tracks('a')))
    render(
      <Provider store={store}>
        <Refill />
      </Provider>,
    )
    await waitFor(() => expect(httpClient).toHaveBeenCalled())

    act(() => {
      store.dispatch(clearQueue())
    })
    await act(async () => answer({ json: { tracks: tracks('late') } }))

    expect(queuedIds(store)).toEqual([])
  })

  it('drops a refill asked for by an earlier Station with the same DJ', async () => {
    let answer
    httpClient.mockReturnValueOnce(new Promise((resolve) => (answer = resolve)))
    httpClient.mockResolvedValue({ json: { tracks: [] } })
    const store = newStore()
    store.dispatch(startStation('sunny', tracks('a')))
    render(
      <Provider store={store}>
        <Refill />
      </Provider>,
    )
    await waitFor(() => expect(httpClient).toHaveBeenCalled())

    act(() => {
      store.dispatch(startStation('sunny', tracks('b', 'c', 'd', 'e')))
    })
    await act(async () => answer({ json: { tracks: tracks('late') } }))

    expect(queuedIds(store)).toEqual(['b', 'c', 'd', 'e'])
  })

  it('tries again on the next Track after a failed refill', async () => {
    httpClient
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValue({ json: { tracks: tracks('d', 'e', 'f') } })
    const store = newStore()
    store.dispatch(startStation('sunny', tracks('a', 'b', 'c')))
    render(
      <Provider store={store}>
        <Refill />
      </Provider>,
    )
    await waitFor(() => expect(httpClient).toHaveBeenCalledTimes(1))

    act(() => playing(store, 1))

    await waitFor(() => expect(queuedIds(store)).toHaveLength(6))
    expect(httpClient).toHaveBeenCalledTimes(2)
  })

  it('keeps trying after a failed refill while the last Track plays', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      httpClient
        .mockRejectedValueOnce(new Error('offline'))
        .mockResolvedValue({ json: { tracks: tracks('b', 'c', 'd') } })
      const store = newStore()
      store.dispatch(startStation('sunny', tracks('a')))
      render(
        <Provider store={store}>
          <Refill />
        </Provider>,
      )
      await waitFor(() => expect(httpClient).toHaveBeenCalledTimes(1))

      await act(async () => {
        vi.advanceTimersByTime(RETRY_MS)
      })

      await waitFor(() => expect(queuedIds(store)).toHaveLength(4))
      expect(httpClient).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
    }
  })
})
