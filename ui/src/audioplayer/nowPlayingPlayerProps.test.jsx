import React from 'react'
import { cleanup, fireEvent, render } from '@testing-library/react'
import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest'
import ReactJkMusicPlayer from 'navidrome-music-player/es/index.js'
import { nowPlayingReducer } from '../nowPlaying/nowPlayingState'
import { nowPlayingPlayerProps } from './nowPlayingPlayerProps'
import useStyle from './styles'

// Under Node, sortablejs resolves to a build without the Swap plugin the
// package imports; the browser build has it.
vi.mock('sortablejs', async () => {
  const sortable = await import('sortablejs/modular/sortable.esm.js')
  return { ...sortable, __esModule: true }
})

const audioLists = [
  {
    musicSrc: '/stream/t1',
    cover: '/cover/t1',
    name: 'Lamplight Freeway',
    singer: 'The Lantern Pilots',
    song: { albumId: 'al1' },
  },
]

// A phone: the package's media query for its mobile layout matches
const phoneScreen = (matches) => {
  window.matchMedia = vi.fn().mockImplementation((query) => ({
    matches,
    media: query,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
}

// The package as Player mounts it, with Player's styles
const StyledPlayer = ({ isDesktop, ...props }) => {
  const classes = useStyle({ visible: true, isDesktop })
  return <ReactJkMusicPlayer className={classes.player} {...props} />
}

const renderPlayer = (isDesktop) => {
  let state = nowPlayingReducer(undefined, { type: '@@INIT' })
  const dispatch = (action) => {
    state = nowPlayingReducer(state, action)
  }
  render(
    <StyledPlayer
      isDesktop={isDesktop}
      audioLists={audioLists}
      autoPlay={false}
      {...nowPlayingPlayerProps(isDesktop, dispatch)}
    />,
  )
  return { isOpen: () => state.open }
}

// A tap on the mini player: the package takes it from its drag handling
const tap = (el) => {
  fireEvent.mouseDown(el, { clientX: 10, clientY: 10 })
  fireEvent.mouseUp(el, { clientX: 10, clientY: 10 })
}

describe('the player package on a phone', () => {
  beforeAll(() => {
    phoneScreen(true)
    HTMLMediaElement.prototype.load = vi.fn()
    HTMLMediaElement.prototype.pause = vi.fn()
    HTMLMediaElement.prototype.play = vi.fn(() => Promise.resolve())
  })
  afterEach(cleanup)

  it('opens Now Playing from the mini player, not its own mobile panel', () => {
    const { isOpen } = renderPlayer(false)
    expect(
      document.querySelector('.react-jinke-music-player-mobile'),
    ).toBeNull()

    tap(document.querySelector('.music-player-controller'))

    expect(isOpen()).toBe(true)
    expect(
      document.querySelector('.react-jinke-music-player-mobile'),
    ).toBeNull()
  })

  it('hides its floating cover for our mini player, and keeps its audio', () => {
    renderPlayer(false)

    const cover = document.querySelector('.react-jinke-music-player')
    expect(getComputedStyle(cover).display).toBe('none')
    expect(document.querySelector('audio')).not.toBeNull()
  })
})

describe('the player package on desktop', () => {
  beforeAll(() => phoneScreen(false))
  afterEach(cleanup)

  it('keeps its full player bar, whose cover opens and closes Now Playing', () => {
    window.location.hash = ''
    const { isOpen } = renderPlayer(true)
    expect(document.querySelector('.music-player-panel')).not.toBeNull()
    const cover = document.querySelector('.music-player-panel .img-content')

    fireEvent.click(cover)
    expect(isOpen()).toBe(true)

    fireEvent.click(cover)
    expect(isOpen()).toBe(false)
    expect(window.location.hash).toBe('')
    const bar = document.querySelector('.music-player-panel')
    expect(getComputedStyle(bar).display).not.toBe('none')
  })
})
