import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { renderHook, act } from '@testing-library/react-hooks'
import { describe, it, expect, vi } from 'vitest'
import { playerReducer } from '../reducers'
import { playTracks } from '../actions'
import { usePlayerRoom } from './usePlayerRoom'

const screenWide = (desktop) => {
  window.matchMedia = vi.fn().mockImplementation((query) => ({
    matches: desktop,
    media: query,
    addListener: vi.fn(),
    removeListener: vi.fn(),
  }))
}

const room = ({ desktop, queued }) => {
  screenWide(desktop)
  const store = createStore(combineReducers({ player: playerReducer }))
  if (queued) {
    act(() => {
      store.dispatch(
        playTracks({ t1: { id: 't1', title: 'Lamplight Freeway' } }),
      )
    })
  }
  const wrapper = ({ children }) => (
    <Provider store={store}>{children}</Provider>
  )
  return renderHook(() => usePlayerRoom(), { wrapper }).result.current
}

describe('usePlayerRoom, the room page content leaves below it for the player', () => {
  it('leaves none on desktop with nothing queued', () => {
    expect(room({ desktop: true, queued: false })).toBe(0)
  })

  it("clears a phone's tab bar, above the home indicator, with nothing queued", () => {
    expect(room({ desktop: false, queued: false })).toBe(
      'calc(env(safe-area-inset-bottom) + 60px)',
    )
  })

  it("clears the desktop player bar's 80px", () => {
    expect(room({ desktop: true, queued: true })).toBe('80px')
  })

  it("clears a phone's mini player and the tab bar below it", () => {
    expect(room({ desktop: false, queued: true })).toBe(
      'calc(env(safe-area-inset-bottom) + 136px)',
    )
  })
})
