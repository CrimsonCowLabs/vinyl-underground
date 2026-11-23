import { describe, it, expect } from 'vitest'
import {
  nowPlayingReducer,
  showNowPlayingTab,
  toggleNowPlayingTab,
} from './nowPlayingState'

const after = (...actions) =>
  actions.reduce(nowPlayingReducer, nowPlayingReducer(undefined, {}))

describe('the tab Now Playing shows', () => {
  it('starts on Art', () => {
    expect(after().tab).toBe('art')
  })

  it('switches to the tab chosen', () => {
    expect(after(showNowPlayingTab('queue')).tab).toBe('queue')
  })

  it('ignores a tab that does not exist', () => {
    expect(after(showNowPlayingTab('karaoke')).tab).toBe('art')
  })

  it("goes back to Art when a tool's own tab is toggled again", () => {
    expect(after(toggleNowPlayingTab('lyrics')).tab).toBe('lyrics')
    expect(
      after(toggleNowPlayingTab('lyrics'), toggleNowPlayingTab('lyrics')).tab,
    ).toBe('art')
    expect(
      after(toggleNowPlayingTab('lyrics'), toggleNowPlayingTab('queue')).tab,
    ).toBe('queue')
  })
})
