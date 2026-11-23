import React from 'react'
import { Provider, useDispatch, useSelector } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { act, cleanup, render } from '@testing-library/react'
import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest'
import ReactJkMusicPlayer from 'navidrome-music-player/es/index.js'
import { playerReducer } from '../../reducers/playerReducer'
import {
  moveInQueue,
  removeFromQueue,
  syncQueue,
  PLAYER_PLAY_TRACKS,
} from '../../actions'

// Under Node, sortablejs resolves to a build without the Swap plugin the
// package imports; the browser build has it.
vi.mock('sortablejs', async () => {
  const sortable = await import('sortablejs/modular/sortable.esm.js')
  return { ...sortable, __esModule: true }
})

// The package wired to the store's queue as Player wires it
const WiredPlayer = ({ onAudio }) => {
  const player = useSelector((state) => state.player)
  const dispatch = useDispatch()
  return (
    <ReactJkMusicPlayer
      mode="full"
      quietUpdate
      autoPlay={false}
      audioLists={player.queue}
      playIndex={player.playIndex}
      clearPriorAudioLists={player.clear}
      onAudioListsChange={(_, lists, info) => dispatch(syncQueue(info, lists))}
      getAudioInstance={onAudio}
    />
  )
}

const queued = (id) => ({
  trackId: id,
  uuid: `u-${id}`,
  name: `Song ${id}`,
  musicSrc: `/stream/${id}`,
})

const setup = () => {
  const store = createStore(combineReducers({ player: playerReducer }), {
    player: {
      queue: ['a', 'b', 'c', 'd'].map(queued),
      current: {},
      playIndex: 1,
      savedPlayIndex: 1,
      clear: true,
      volume: 1,
    },
  })
  let audio
  render(
    <Provider store={store}>
      <WiredPlayer onAudio={(a) => (audio = a)} />
    </Provider>,
  )
  // The package's own list, as it would play it
  const packageOrder = () =>
    Array.from(document.querySelectorAll('.audio-lists-panel .audio-item')).map(
      (el) => el.textContent.match(/Song \w/)[0],
    )
  return { store, audio: () => audio, packageOrder }
}

describe('queue edits reach the player package', () => {
  beforeAll(() => {
    window.matchMedia = (query) => ({
      matches: false,
      media: query,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
    })
    HTMLMediaElement.prototype.load = vi.fn()
    HTMLMediaElement.prototype.pause = vi.fn()
    HTMLMediaElement.prototype.play = vi.fn(() => Promise.resolve())
  })
  // The package sets up its list's drag sorting again 200ms after a change,
  // which must happen before it is unmounted
  afterEach(async () => {
    await act(() => new Promise((r) => setTimeout(r, 250)))
    cleanup()
  })

  it('reorders its list, the Track loaded staying loaded', async () => {
    const { store, audio, packageOrder } = setup()
    await act(() => new Promise((r) => setTimeout(r, 50)))
    const src = audio().src
    expect(src).toContain('/stream/b')
    expect(packageOrder()).toEqual(['Song a', 'Song b', 'Song c', 'Song d'])

    act(() => store.dispatch(moveInQueue(3, 0)))
    await act(() => new Promise((r) => setTimeout(r, 50)))

    expect(packageOrder()).toEqual(['Song d', 'Song a', 'Song b', 'Song c'])
    expect(audio().src).toBe(src)
    expect(store.getState().player.queue.map((t) => t.name)).toEqual(
      packageOrder(),
    )
  })

  it('drops a removed Track from its list', async () => {
    const { store, audio, packageOrder } = setup()
    await act(() => new Promise((r) => setTimeout(r, 50)))
    const src = audio().src

    act(() => store.dispatch(removeFromQueue(2)))
    await act(() => new Promise((r) => setTimeout(r, 50)))

    expect(packageOrder()).toEqual(['Song a', 'Song b', 'Song d'])
    expect(audio().src).toBe(src)
  })

  it('a new album played afterwards still replaces the list', async () => {
    const { store, packageOrder } = setup()
    await act(() => new Promise((r) => setTimeout(r, 50)))
    act(() => store.dispatch(moveInQueue(3, 0)))
    await act(() => new Promise((r) => setTimeout(r, 50)))

    act(() =>
      store.dispatch({
        type: PLAYER_PLAY_TRACKS,
        id: 'x',
        data: { x: { id: 'x', title: 'Song x' } },
      }),
    )
    await act(() => new Promise((r) => setTimeout(r, 50)))
    expect(packageOrder()).toEqual(['Song x'])
  })
})
