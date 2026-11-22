import { describe, it, expect } from 'vitest'
import { playerReducer } from '../../reducers/playerReducer'
import {
  currentPlaying,
  moveInQueue,
  playFromQueue,
  removeFromQueue,
  syncQueue,
} from '../../actions'

const track = (id) => ({ trackId: id, uuid: `u-${id}`, name: `Song ${id}` })

// A queue of a, b, c, d with the Track at `playing` playing, settled as the
// player package leaves it (no switch pending)
const queueOf = (playing = 1) => {
  const queue = ['a', 'b', 'c', 'd'].map(track)
  return {
    queue,
    current: queue[playing],
    savedPlayIndex: playing,
    playIndex: undefined,
    clear: false,
    volume: 1,
  }
}

const names = (state) => state.queue.map((t) => t.trackId).join('')

describe('playing a Track from the queue', () => {
  it('asks the player to switch to it', () => {
    const state = playerReducer(queueOf(), playFromQueue(3))
    expect(state.playIndex).toBe(3)
    expect(names(state)).toBe('abcd')
  })

  it('keeps the switch pending until the player starts the Track', () => {
    let state = playerReducer(queueOf(), playFromQueue(3))
    // the player reports the old Track pausing as it switches
    state = playerReducer(state, currentPlaying(state.queue[1]))
    expect(state.playIndex).toBe(3)
    state = playerReducer(state, currentPlaying(state.queue[3]))
    expect(state.playIndex).toBeUndefined()
    expect(state.savedPlayIndex).toBe(3)
  })
})

describe('moving a Track in the queue', () => {
  it('reorders the queue and hands the whole list to the player', () => {
    const state = playerReducer(queueOf(), moveInQueue(3, 0))
    expect(names(state)).toBe('dabc')
    // clear makes the package take the new order rather than append to its own
    expect(state.clear).toBe(true)
  })

  it('keeps the same Track playing, wherever it lands', () => {
    let state = playerReducer(queueOf(1), moveInQueue(0, 3))
    expect(names(state)).toBe('bcda')
    expect(state.savedPlayIndex).toBe(0)

    state = playerReducer(queueOf(1), moveInQueue(1, 2))
    expect(names(state)).toBe('acbd')
    expect(state.savedPlayIndex).toBe(2)
    expect(state.playIndex).toBeUndefined()
  })

  it('settles once the player has the new list', () => {
    let state = playerReducer(queueOf(), moveInQueue(3, 0))
    state = playerReducer(state, syncQueue({}, state.queue))
    expect(state.clear).toBe(false)
    expect(state.playIndex).toBeUndefined()
  })

  it('keeps a queued Track queued after a reload, before anything plays', () => {
    // after a reload the player is told to load the saved Track
    const reloaded = { ...queueOf(2), current: {}, playIndex: 2 }
    const state = playerReducer(reloaded, moveInQueue(2, 0))
    expect(names(state)).toBe('cabd')
    expect(state.savedPlayIndex).toBe(0)
    expect(state.playIndex).toBe(0)
  })

  it('ignores a move to the same place', () => {
    const before = queueOf()
    expect(playerReducer(before, moveInQueue(2, 2))).toBe(before)
  })
})

describe('removing a Track from the queue', () => {
  it('takes out a Track other than the one playing, which plays on', () => {
    const state = playerReducer(queueOf(2), removeFromQueue(0))
    expect(names(state)).toBe('bcd')
    expect(state.savedPlayIndex).toBe(1)
    expect(state.playIndex).toBeUndefined()
    expect(state.clear).toBe(true)
  })

  it('moves on to the next Track when the one playing is removed', () => {
    const state = playerReducer(queueOf(1), removeFromQueue(1))
    expect(names(state)).toBe('acd')
    expect(state.playIndex).toBe(1)
    expect(state.queue[state.playIndex].trackId).toBe('c')
  })

  it('goes back to the Track before when the last one, playing, is removed', () => {
    const state = playerReducer(queueOf(3), removeFromQueue(3))
    expect(names(state)).toBe('abc')
    expect(state.playIndex).toBe(2)
  })

  it('never empties the queue', () => {
    const only = { ...queueOf(0), queue: [track('a')] }
    expect(playerReducer(only, removeFromQueue(0))).toBe(only)
  })
})
