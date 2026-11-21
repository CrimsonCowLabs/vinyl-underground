import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { render, act, cleanup } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { playerReducer } from '../reducers'
import { clearQueue } from '../actions'
import { startStation, stationReducer } from './stationState'
import { useStationSkips } from './useStationSkips'

const httpClient = vi.fn()
vi.mock('../dataProvider', () => ({
  httpClient: (...args) => httpClient(...args),
}))

const tracks = (...ids) => ids.map((id) => ({ id, title: id, artist: id }))

const newStore = () =>
  createStore(
    combineReducers({ player: playerReducer, station: stationReducer }),
  )

// mount renders the hook and returns what it gives the Player.
const mount = (store) => {
  const res = {}
  const Skips = () => {
    Object.assign(res, useStationSkips())
    return null
  }
  render(
    <Provider store={store}>
      <Skips />
    </Provider>,
  )
  return res
}

const skipsSent = () =>
  httpClient.mock.calls
    .filter(([url]) => url.endsWith('/station/skip'))
    .map(([, opts]) => JSON.parse(opts.body))

describe('useStationSkips', () => {
  let store, player

  beforeEach(() => {
    httpClient.mockReset()
    httpClient.mockResolvedValue({})
    store = newStore()
    act(() => {
      store.dispatch(startStation('sunny', tracks('a', 'b', 'c')))
    })
    player = mount(store)
  })
  afterEach(cleanup)

  it('reports a Station Track left before half of it played as a skip', () => {
    player.trackStarted('a', 200)
    player.trackLeft('a', 30000)
    expect(skipsSent()).toEqual([{ dj: 'sunny', track: 'a' }])
  })

  it('does not count a Track that mostly played as a skip', () => {
    player.trackStarted('a', 200)
    player.trackLeft('a', 150000)
    expect(skipsSent()).toEqual([])
  })

  it('does not count leaving a Track once the Station ended', () => {
    player.trackStarted('a', 200)
    act(() => {
      store.dispatch(clearQueue())
    })
    player.trackLeft('a', 1000)
    expect(skipsSent()).toEqual([])
  })

  it('does not count starting another Station as a skip on it', () => {
    player.trackStarted('a', 200)
    act(() => {
      store.dispatch(startStation('night-owl', tracks('x')))
    })
    player.trackLeft('a', 1000)
    expect(skipsSent()).toEqual([])
  })

  it('does not report Tracks played before the Station started', () => {
    cleanup()
    store = newStore()
    player = mount(store)
    player.trackStarted('a', 200)
    act(() => {
      store.dispatch(startStation('sunny', tracks('b')))
    })
    player.trackLeft('a', 1000)
    expect(skipsSent()).toEqual([])
  })
})
