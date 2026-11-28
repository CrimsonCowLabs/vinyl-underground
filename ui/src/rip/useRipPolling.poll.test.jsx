import { renderHook } from '@testing-library/react-hooks'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import * as RA from 'react-admin'
import { useRipPolling, IDLE_POLL_MS } from './useRipPolling'

vi.mock('react-admin', async () => {
  const actual = await vi.importActual('react-admin')
  return {
    ...actual,
    useListContext: vi.fn(),
    useRefresh: vi.fn(),
  }
})

// react-admin 3 keys a list's content on its refresh counter, so a refresh remounts every
// expanded review card and drops what it holds only in itself (a MusicBrainz search and the
// release picked from it). The poll fetches the jobs again instead, leaving the cards mounted.
describe('useRipPolling', () => {
  const refetch = vi.fn()
  const refresh = vi.fn()

  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    RA.useRefresh.mockReturnValue(refresh)
  })
  afterEach(() => vi.useRealTimers())

  it('fetches the jobs again, without a refresh that would remount the cards', () => {
    RA.useListContext.mockReturnValue({ data: {}, refetch })
    renderHook(() => useRipPolling())
    vi.advanceTimersByTime(IDLE_POLL_MS)
    expect(refetch).toHaveBeenCalledTimes(1)
    expect(refresh).not.toHaveBeenCalled()
  })

  it('looks again more often while a Rip is downloading', () => {
    RA.useListContext.mockReturnValue({
      data: { a: { status: 'downloading' } },
      refetch,
    })
    renderHook(() => useRipPolling())
    vi.advanceTimersByTime(IDLE_POLL_MS)
    expect(refetch.mock.calls.length).toBeGreaterThan(1)
    expect(refresh).not.toHaveBeenCalled()
  })
})
