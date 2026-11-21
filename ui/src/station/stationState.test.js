import { describe, it, expect } from 'vitest'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { playerReducer } from '../reducers'
import { addTracks, clearQueue, playTracks, setTrack } from '../actions'
import {
  addStationTracks,
  startStation,
  stationReducer,
  upcomingTracks,
} from './stationState'

const tracks = [
  { id: 'mf-1', title: 'Blue in Green', artist: 'Miles Davis' },
  { id: 'mf-2', title: 'Naima', artist: 'John Coltrane' },
]

const newStore = () =>
  createStore(
    combineReducers({ player: playerReducer, station: stationReducer }),
  )

describe('startStation', () => {
  it('replaces the play queue with the Station Tracks and puts the DJ on air', () => {
    const store = newStore()
    store.dispatch(playTracks({ old: { id: 'old', title: 'Old' } }))

    store.dispatch(startStation('night-owl', tracks))

    const { player, station } = store.getState()
    expect(player.queue.map((item) => item.trackId)).toEqual(['mf-1', 'mf-2'])
    expect(player.playIndex).toBe(0)
    expect(station.djId).toBe('night-owl')
  })

  it('ends the Station when the Listener plays something else', () => {
    for (const playSomethingElse of [
      () => playTracks({ x: { id: 'x', title: 'X' } }),
      () => setTrack({ id: 'x', title: 'X' }),
      () => clearQueue(),
    ]) {
      const store = newStore()
      store.dispatch(startStation('sunny', tracks))
      store.dispatch(playSomethingElse())
      expect(store.getState().station.djId).toBeNull()
    }
  })

  it('keeps the Station on air when Tracks are added to its queue', () => {
    const store = newStore()
    store.dispatch(startStation('sunny', tracks))
    store.dispatch(addTracks({ x: { id: 'x', title: 'X' } }))
    expect(store.getState().station.djId).toBe('sunny')
  })

  it('queues a Track the server served twice, as a tiny Library must', () => {
    const store = newStore()
    const solo = { id: 'mf-1', title: 'Solo', artist: 'Solo' }
    store.dispatch(startStation('sunny', [solo, solo]))
    store.dispatch(addStationTracks([solo, solo]))
    expect(store.getState().player.queue.map((i) => i.trackId)).toEqual([
      'mf-1',
      'mf-1',
      'mf-1',
      'mf-1',
    ])
  })
})

describe('upcomingTracks', () => {
  const queue = [{ uuid: 'a' }, { uuid: 'b' }, { uuid: 'c' }, { uuid: 'd' }]

  it('counts the Tracks queued after the one playing', () => {
    expect(upcomingTracks({ queue, current: { uuid: 'b' } })).toBe(2)
    expect(upcomingTracks({ queue, current: { uuid: 'd' } })).toBe(0)
  })

  it('counts from the Track about to play when nothing is playing yet', () => {
    expect(upcomingTracks({ queue, current: {}, playIndex: 0 })).toBe(3)
    expect(upcomingTracks({ queue, current: {} })).toBe(3)
  })

  it('is 0 for an empty queue', () => {
    expect(upcomingTracks({ queue: [], current: {} })).toBe(0)
  })
})
